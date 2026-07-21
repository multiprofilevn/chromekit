package chromekit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
)

// chromedp dispatches events on a single goroutine per target. Anything this
// package runs inline on that goroutine blocks every CDP reply for the tab —
// including the acknowledgements an in-flight mouse gesture is waiting on.

func newListenerTestPage() *Page {
	ctx, cancel := context.WithCancel(context.Background())
	return &Page{id: "p", ctx: ctx, cancel: cancel, log: noopLogger{}, cfg: defaultConfig()}
}

// setConsoleHandler wires a handler the way OnConsole does, minus
// initListeners — these tests drive dispatch directly and have no live CDP
// connection for chromedp.ListenTarget to bind to.
func (p *Page) setConsoleHandler(h func(ConsoleEvent)) {
	p.listenerMu.Lock()
	p.consoleHandler = h
	p.consoleSem = make(chan struct{}, maxConcurrentConsoleHandlers)
	p.listenerMu.Unlock()
}

func TestHandleConsole_DoesNotRunHandlerInline(t *testing.T) {
	p := newListenerTestPage()
	defer p.cancel()

	started := make(chan struct{})
	release := make(chan struct{})
	p.setConsoleHandler(func(ConsoleEvent) {
		close(started)
		<-release
	})

	done := make(chan struct{})
	go func() {
		p.handleConsole(&runtime.EventConsoleAPICalled{Type: "log"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleConsole blocked on the user handler — the dispatch goroutine is frozen")
	}
	<-started
	close(release)
}

func TestHandleConsole_BoundsConcurrentHandlers(t *testing.T) {
	p := newListenerTestPage()
	defer p.cancel()

	var inFlight, peak int64
	release := make(chan struct{})
	var once sync.Once
	p.setConsoleHandler(func(ConsoleEvent) {
		n := atomic.AddInt64(&inFlight, 1)
		for {
			old := atomic.LoadInt64(&peak)
			if n <= old || atomic.CompareAndSwapInt64(&peak, old, n) {
				break
			}
		}
		<-release
		atomic.AddInt64(&inFlight, -1)
	})

	// Far more events than the semaphore allows; the excess must be dropped,
	// not queued into a goroutine each.
	for i := 0; i < 500; i++ {
		p.handleConsole(&runtime.EventConsoleAPICalled{Type: "log"})
	}
	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt64(&peak); got > maxConcurrentConsoleHandlers {
		t.Fatalf("%d handlers ran concurrently, cap is %d — a console loop can exhaust goroutines",
			got, maxConcurrentConsoleHandlers)
	}
	once.Do(func() { close(release) })
	time.Sleep(50 * time.Millisecond)
}

func TestTruncateConsoleText(t *testing.T) {
	short := "hello"
	if got := truncateConsoleText(short); got != short {
		t.Errorf("short text altered: %q", got)
	}
	huge := make([]byte, maxConsoleTextLen*4)
	for i := range huge {
		huge[i] = 'x'
	}
	got := truncateConsoleText(string(huge))
	if len(got) > maxConsoleTextLen+32 {
		t.Fatalf("truncated length %d, want ≈%d — page controls this allocation", len(got), maxConsoleTextLen)
	}
}

func TestDropFinishedDownloads(t *testing.T) {
	// Only complete Begin→Done runs belong to downloads nobody waited on. A
	// trailing run with no Done is the download the caller is waiting for
	// right now, whose events simply arrived first.
	cases := []struct {
		name string
		in   []downloadEvent
		want []downloadEvent
	}{
		{"empty", nil, nil},
		{
			"one finished download is dropped",
			[]downloadEvent{{Filename: "old.zip"}, {Done: true}},
			nil,
		},
		{
			"two finished downloads are dropped",
			[]downloadEvent{{Filename: "a.zip"}, {Done: true}, {Filename: "b.zip"}, {Done: true}},
			nil,
		},
		{
			"in-flight download is kept",
			[]downloadEvent{{Filename: "current.pdf"}},
			[]downloadEvent{{Filename: "current.pdf"}},
		},
		{
			"finished download dropped, in-flight kept",
			[]downloadEvent{{Filename: "old.zip"}, {Done: true}, {Filename: "current.pdf"}},
			[]downloadEvent{{Filename: "current.pdf"}},
		},
		{
			"fully arrived current download is kept",
			[]downloadEvent{{Filename: "current.pdf"}, {Done: true}},
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dropFinishedDownloads(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d events %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("event %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDownloadWait_KeepsEventsThatArrivedBeforeTheCall(t *testing.T) {
	// The documented usage is "trigger, then call Wait", and Chrome reports
	// downloadWillBegin asynchronously — so the events for the very download
	// being waited on routinely land in the channel first. Draining them
	// blindly turns a successful download into a timeout.
	p := newListenerTestPage()
	defer p.cancel()
	p.listenerMu.Lock()
	p.downloadCh = make(chan downloadEvent, downloadChBuf)
	p.listenerMu.Unlock()

	p.downloadCh <- downloadEvent{Filename: "report.pdf"}

	api := &DownloadsAPI{p: p}
	go func() {
		time.Sleep(50 * time.Millisecond)
		p.downloadChan() <- downloadEvent{Done: true}
	}()

	got, err := api.Wait(3 * time.Second)
	if err != nil {
		t.Fatalf("Wait err = %v, want nil", err)
	}
	if got != "report.pdf" {
		t.Fatalf("Wait = %q, want report.pdf", got)
	}
}

func TestDownloadWait_DrainsStaleEvents(t *testing.T) {
	// A download nobody waited on leaves its events queued. The next Wait must
	// not resolve with the previous download's filename.
	p := newListenerTestPage()
	defer p.cancel()
	p.listenerMu.Lock()
	p.downloadCh = make(chan downloadEvent, downloadChBuf)
	p.listenerMu.Unlock()

	p.downloadCh <- downloadEvent{Filename: "stale.zip"}
	p.downloadCh <- downloadEvent{Done: true}

	api := &DownloadsAPI{p: p}
	go func() {
		time.Sleep(100 * time.Millisecond)
		ch := p.downloadChan()
		ch <- downloadEvent{Filename: "fresh.zip"}
		ch <- downloadEvent{Done: true}
	}()

	got, err := api.Wait(3 * time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got != "fresh.zip" {
		t.Fatalf("Wait returned %q, want fresh.zip — stale events were not drained", got)
	}
}

func TestDownloadChannelSwapIsRaceFree(t *testing.T) {
	p := newListenerTestPage()
	defer p.cancel()
	p.listenerMu.Lock()
	p.downloadCh = make(chan downloadEvent, downloadChBuf)
	p.listenerMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			p.listenerMu.Lock()
			p.downloadCh = make(chan downloadEvent, downloadChBuf)
			p.listenerMu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if ch := p.downloadChan(); ch != nil {
				select {
				case ch <- downloadEvent{Filename: "x"}:
				default:
				}
			}
		}
	}()
	wg.Wait()
}

func TestRebuildFrames_KeepsDirtyWhenTreeChangedMidFetch(t *testing.T) {
	// The generation counter is what stops a fetch that raced an invalidation
	// from marking a stale cache clean.
	p := newListenerTestPage()
	defer p.cancel()

	p.frameMu.RLock()
	gen := p.frameGen
	p.frameMu.RUnlock()

	p.markFramesDirty()

	p.frameMu.RLock()
	newGen, dirty := p.frameGen, p.framesDirty
	p.frameMu.RUnlock()

	if newGen == gen {
		t.Fatal("markFramesDirty did not bump the generation counter")
	}
	if !dirty {
		t.Fatal("markFramesDirty did not set the dirty flag")
	}
}

func TestDetachFrame_FlipsAttached(t *testing.T) {
	p := newListenerTestPage()
	defer p.cancel()
	p.frameMu.Lock()
	p.framesCache = []*Frame{
		{id: "keep", page: p, attached: true},
		{id: "gone", page: p, attached: true},
	}
	p.frameMu.Unlock()

	p.detachFrame("gone")

	p.frameMu.RLock()
	defer p.frameMu.RUnlock()
	for _, f := range p.framesCache {
		want := f.id == "keep"
		if f.attached != want {
			t.Errorf("frame %q attached = %v, want %v", f.id, f.attached, want)
		}
	}
}

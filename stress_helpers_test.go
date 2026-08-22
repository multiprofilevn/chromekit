//go:build integration || stress

package chromekit

import (
	"context"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multiprofilevn/chromekit/input"
)

// resolveChromePath returns a usable chrome.exe path for integration/stress
// tests. Order: env CHROME_PATH > common Windows install dirs > skip.
func resolveChromePath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	if runtime.GOOS != "windows" {
		t.Skip("integration test currently expects Windows chrome.exe — set CHROME_PATH to override")
	}
	candidates := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Skip("no chrome.exe found in standard locations; set CHROME_PATH to run this test")
	return ""
}

// stableGoroutineCount samples the goroutine count after a GC, so counts taken
// before and after a workload are comparable.
func stableGoroutineCount() int {
	runtime.GC()
	return runtime.NumGoroutine()
}

// awaitGoroutinesAtMost polls until the goroutine count settles at or below max.
// chromedp tears targets down asynchronously, so a single sample taken right
// after an eviction is inherently flaky.
func awaitGoroutinesAtMost(max int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if stableGoroutineCount() <= max {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// awaitPageCount polls until the browser tracks want pages, or fails the test.
func awaitPageCount(t *testing.T, b *Browser, want int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if len(b.Pages()) == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("page count = %d, want %d after %s", len(b.Pages()), want, within)
}

// --- stress metrics -------------------------------------------------------

// countingCDPBackend wraps a real input.Backend and tallies CDP dispatches by
// kind, so the stress harness can report mouse round-trip volume without
// instrumenting production code.
type countingCDPBackend struct {
	input.Backend
	moves  int64
	clicks int64
	other  int64
}

func (b *countingCDPBackend) MouseMove(ctx context.Context, x, y float64) error {
	atomic.AddInt64(&b.moves, 1)
	return b.Backend.MouseMove(ctx, x, y)
}

func (b *countingCDPBackend) MouseDown(ctx context.Context, x, y float64, btn input.MouseButton) error {
	atomic.AddInt64(&b.clicks, 1)
	return b.Backend.MouseDown(ctx, x, y, btn)
}

func (b *countingCDPBackend) MouseScroll(ctx context.Context, dx, dy float64) error {
	atomic.AddInt64(&b.other, 1)
	return b.Backend.MouseScroll(ctx, dx, dy)
}

// injectCountingBackend swaps a page's input stack for one that dispatches
// through cb, before any input method has lazily built the real stack. Returns
// false if the page already initialised its input (call right after NewPage).
func injectCountingBackend(p *Page, profile input.Profile) *countingCDPBackend {
	cb := &countingCDPBackend{Backend: input.New(input.TypeCDP, input.Config{})}
	p.inputOnce.Do(func() {
		p.backend = cb
		p.cursor = input.NewCursorWithProfile(cb, profile)
		p.keyboard = input.NewKeyboard(cb)
		p.touch = input.NewTouchScreen(cb)
	})
	return cb
}

// latencyRecorder collects per-operation durations and reports percentiles.
type latencyRecorder struct {
	mu   sync.Mutex
	data map[string][]time.Duration
	errs map[string]int64
}

func newLatencyRecorder() *latencyRecorder {
	return &latencyRecorder{data: map[string][]time.Duration{}, errs: map[string]int64{}}
}

func (r *latencyRecorder) record(op string, d time.Duration, err error) {
	r.mu.Lock()
	r.data[op] = append(r.data[op], d)
	if err != nil {
		r.errs[op]++
	}
	r.mu.Unlock()
}

type latStat struct {
	op                 string
	n                  int
	errs               int64
	p50, p95, max      time.Duration
}

func (r *latencyRecorder) stats() []latStat {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]latStat, 0, len(r.data))
	for op, ds := range r.data {
		cp := append([]time.Duration(nil), ds...)
		sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
		out = append(out, latStat{
			op:   op,
			n:    len(cp),
			errs: r.errs[op],
			p50:  pct(cp, 0.50),
			p95:  pct(cp, 0.95),
			max:  cp[len(cp)-1],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].op < out[j].op })
	return out
}

func pct(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q * float64(len(sorted)-1))
	return sorted[idx]
}

// resourceSampler tracks goroutine high-water and peak heap on a ticker until
// stopped.
type resourceSampler struct {
	stop         chan struct{}
	done         chan struct{}
	goroutineHWM int32
	heapPeak     uint64
}

func startResourceSampler(every time.Duration) *resourceSampler {
	s := &resourceSampler{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		t := time.NewTicker(every)
		defer t.Stop()
		var ms runtime.MemStats
		for {
			if g := int32(runtime.NumGoroutine()); g > s.goroutineHWM {
				s.goroutineHWM = g
			}
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > s.heapPeak {
				s.heapPeak = ms.HeapAlloc
			}
			select {
			case <-s.stop:
				return
			case <-t.C:
			}
		}
	}()
	return s
}

func (s *resourceSampler) Stop() {
	close(s.stop)
	<-s.done
}

package chromekit

import (
	"context"
	"testing"
	"time"
)

func TestBrowserDone_MemoizedSingleChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &Browser{ctx: ctx}

	first := b.Done()
	for i := 0; i < 1000; i++ {
		if b.Done() != first {
			t.Fatal("Done() returned a different channel — watcher not memoized")
		}
	}

	cancel()
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Fatal("Done channel not closed after ctx cancel")
	}
}

func TestBrowserDone_ClosesOnSignalClosed(t *testing.T) {
	b := &Browser{ctx: context.Background()}
	done := b.Done()
	b.signalClosed()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Done channel not closed after signalClosed")
	}
}

func TestBrowserDone_NilSafe(t *testing.T) {
	var b *Browser
	select {
	case <-b.Done():
	case <-time.After(time.Second):
		t.Fatal("nil browser Done should already be closed")
	}

	b2 := &Browser{} // nil ctx
	select {
	case <-b2.Done():
	case <-time.After(time.Second):
		t.Fatal("browser with nil ctx Done should already be closed")
	}
}

func TestIsClosedLocked(t *testing.T) {
	b := &Browser{ctx: context.Background()}

	b.pagesMu.Lock()
	if b.isClosedLocked() {
		b.pagesMu.Unlock()
		t.Fatal("fresh browser reported closed")
	}
	b.pagesMu.Unlock()

	b.signalClosed()

	b.pagesMu.Lock()
	closed := b.isClosedLocked()
	b.pagesMu.Unlock()
	if !closed {
		t.Fatal("after signalClosed, isClosedLocked should be true")
	}
}

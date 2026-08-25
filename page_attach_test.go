package chromekit

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// A page whose CDP session is already attached must not touch chromedp again:
// the memoised path is what keeps alive() cheap for every subsequent operation.
func TestEnsureAttachedMemoisesAfterSuccess(t *testing.T) {
	// A plain context is not a chromedp context, so any real attach attempt
	// fails immediately — reaching one here would surface as a non-nil error.
	p := &Page{id: "memo", ctx: context.Background(), attached: true}

	if err := p.ensureAttached(); err != nil {
		t.Fatalf("ensureAttached on an already attached page: %v", err)
	}
}

// alive() reports a dead page as canceled rather than as an attach failure —
// callers distinguish "the tab is gone" from "the tab refused the command".
func TestAliveReportsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &Page{id: "dead", ctx: ctx}

	if err := p.alive(); !errors.Is(err, ErrContextCanceled) {
		t.Fatalf("alive() on a canceled page = %v, want ErrContextCanceled", err)
	}
}

// A live page that cannot attach must surface the failure instead of pretending
// the operation may proceed.
func TestAliveReportsAttachFailure(t *testing.T) {
	p := &Page{id: "unattachable", ctx: context.Background()}

	err := p.alive()
	if err == nil {
		t.Fatal("alive() on a page that cannot attach = nil, want an error")
	}
	if errors.Is(err, ErrContextCanceled) {
		t.Fatalf("alive() = %v, want an attach failure, not a cancellation", err)
	}
}

// The whole point of the guard: the lazy first attach must never run on two
// goroutines at once. chromedp stores the session in an unsynchronised field
// that its own cancellation goroutine reads, and overlapping attempts crash the
// process. Run under -race, which is what actually polices the invariant.
func TestAliveConcurrentCallersDoNotRace(t *testing.T) {
	p := &Page{id: "concurrent", ctx: context.Background()}

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.alive()
		}()
	}
	wg.Wait()

	if p.attached {
		t.Fatal("page marked attached although no attach could have succeeded")
	}
}

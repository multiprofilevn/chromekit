package chromekit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/multiprofilevn/chromekit/input"
)

// ctxCapturingBackend records the context of the first MouseMove so a test can
// inspect the deadline the MouseAPI layer attached. Optionally blocks until the
// context is done to exercise cancellation.
type ctxCapturingBackend struct {
	mu          sync.Mutex
	hasDeadline bool
	deadline    time.Time
	captured    bool
	block       bool
}

func (b *ctxCapturingBackend) MouseMove(ctx context.Context, x, y float64) error {
	b.mu.Lock()
	if !b.captured {
		b.deadline, b.hasDeadline = ctx.Deadline()
		b.captured = true
	}
	b.mu.Unlock()
	if b.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (b *ctxCapturingBackend) MouseDown(context.Context, float64, float64, input.MouseButton) error {
	return nil
}
func (b *ctxCapturingBackend) MouseUp(context.Context, float64, float64, input.MouseButton) error {
	return nil
}
func (b *ctxCapturingBackend) MouseScroll(context.Context, float64, float64) error { return nil }
func (b *ctxCapturingBackend) KeyChar(context.Context, rune) error                 { return nil }
func (b *ctxCapturingBackend) InsertText(context.Context, string) error            { return nil }
func (b *ctxCapturingBackend) KeyCombo(context.Context, uint16, input.Modifier) error {
	return nil
}

// newStubMousePage builds a Page whose input stack is the given backend, without
// a real Chrome connection. ProfileMachine keeps MoveTo to a single dispatch.
func newStubMousePage(ctx context.Context, be input.Backend) *Page {
	// attached: the stub stands in for a live page, so alive() must not try to
	// drive chromedp's lazy attach against this bare context.
	p := &Page{ctx: ctx, cfg: defaultConfig(), attached: true}
	p.mouseAPI = &MouseAPI{p: p}
	// Consume inputOnce so the real initInput() (which builds a CDP backend
	// needing a live browser) never runs and overwrites our stub.
	p.inputOnce.Do(func() {
		p.backend = be
		p.cursor = input.NewCursorWithProfile(be, input.ProfileMachine)
		p.touch = input.NewTouchScreen(be)
	})
	return p
}

// TestMouseAPI_AppliesActionDeadline proves the mouse layer wraps p.ctx in
// opCtx: even with a parent context that never expires, the backend receives a
// context bounded by DefaultActionTimeout. This is the fix for gestures hanging
// indefinitely (they previously passed the raw, deadline-less page context).
func TestMouseAPI_AppliesActionDeadline(t *testing.T) {
	be := &ctxCapturingBackend{}
	p := newStubMousePage(context.Background(), be)

	if err := p.Mouse().MoveTo(100, 100); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if !be.hasDeadline {
		t.Fatal("backend context has no deadline — opCtx not applied")
	}
	d := time.Until(be.deadline)
	if d < 25*time.Second || d > DefaultActionTimeout+time.Second {
		t.Fatalf("deadline %v not within expected ~%v window", d, DefaultActionTimeout)
	}
}

// TestMouseAPI_HonorsCancellation proves a wedged renderer cannot hang a gesture
// forever: with a blocking backend and a short-lived parent context, MoveTo
// returns promptly with the context error instead of blocking indefinitely.
func TestMouseAPI_HonorsCancellation(t *testing.T) {
	be := &ctxCapturingBackend{block: true}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	p := newStubMousePage(ctx, be)

	done := make(chan error, 1)
	go func() { done <- p.Mouse().MoveTo(100, 100) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("MoveTo should return the context error when the backend is wedged")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("MoveTo hung past the parent deadline — not bounded")
	}
}

// SetWindow is unused by this fake — the frame lives in the real native backend.
func (*ctxCapturingBackend) SetWindow(input.Window) {}

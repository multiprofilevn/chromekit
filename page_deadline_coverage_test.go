package chromekit

import (
	"context"
	"testing"
	"time"
)

// Every user-facing operation must be bounded. Pre-fix, Navigate's timeout
// only covered the wait phase, and Click/Type had no deadline at all: a tab
// whose renderer stopped acknowledging CDP left the calling goroutine parked
// until the whole browser died.

func TestOpCtx(t *testing.T) {
	t.Run("zero uses the default", func(t *testing.T) {
		ctx, cancel := opCtx(context.Background(), 0)
		defer cancel()
		dl, ok := ctx.Deadline()
		if !ok {
			t.Fatal("opCtx(0) produced a context with no deadline")
		}
		if d := time.Until(dl); d < DefaultActionTimeout-time.Second || d > DefaultActionTimeout+time.Second {
			t.Errorf("opCtx(0) deadline in %s, want ~%s", d, DefaultActionTimeout)
		}
	})

	t.Run("negative uses the default", func(t *testing.T) {
		ctx, cancel := opCtx(context.Background(), -5*time.Second)
		defer cancel()
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("opCtx(negative) produced a context with no deadline")
		}
	})

	t.Run("explicit timeout is honored", func(t *testing.T) {
		ctx, cancel := opCtx(context.Background(), 250*time.Millisecond)
		defer cancel()
		dl, ok := ctx.Deadline()
		if !ok {
			t.Fatal("no deadline")
		}
		if d := time.Until(dl); d > time.Second {
			t.Errorf("deadline in %s, want ~250ms", d)
		}
	})

	t.Run("inherits parent cancellation", func(t *testing.T) {
		parent, parentCancel := context.WithCancel(context.Background())
		ctx, cancel := opCtx(parent, time.Hour)
		defer cancel()

		parentCancel()
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("opCtx context did not follow its parent's cancellation")
		}
	})
}

func TestNavigate_BoundedOnDeadTab(t *testing.T) {
	// A cancelled page context stands in for a renderer that stopped
	// answering: the call must return promptly rather than block.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &Page{id: "dead", ctx: ctx, cancel: cancel, log: noopLogger{}, cfg: defaultConfig()}

	for name, call := range map[string]func() error{
		"Navigate":  func() error { return p.Navigate("about:blank", time.Second) },
		"Reload":    func() error { return p.Reload(time.Second) },
		"GoBack":    func() error { return p.GoBack(time.Second) },
		"GoForward": func() error { return p.GoForward(time.Second) },
		"Click":     func() error { return p.Click("#nope") },
		"Type":      func() error { return p.Type("#nope", "x") },
	} {
		t.Run(name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- call() }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("%s on a dead page returned nil, want an error", name)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s blocked on a dead page — no deadline covers the operation", name)
			}
		})
	}
}

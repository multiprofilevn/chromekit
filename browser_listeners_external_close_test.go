package chromekit

import (
	"context"
	"testing"
)

// Tabs can die outside chromekit's control: window.close(), the user closing a
// tab in Connect mode, an OAuth popup closing itself after posting its result.
// Without a browser-level targetDestroyed subscription their Page, context and
// chromedp target goroutines live until the whole browser shuts down.

func newTestBrowser(t *testing.T) (*Browser, context.Context) {
	t.Helper()
	bctx, bcancel := context.WithCancel(context.Background())
	t.Cleanup(bcancel)
	b := &Browser{log: noopLogger{}, cfg: defaultConfig(), ctx: bctx, cancel: bcancel}
	return b, bctx
}

func (b *Browser) addTestPage(t *testing.T, id string, primary bool) (*Page, context.Context) {
	t.Helper()
	if primary {
		p := &Page{id: id, ctx: b.ctx, cancel: b.cancel, primary: true, log: b.log, browser: b}
		b.pages = append(b.pages, p)
		b.current = p
		return p, b.ctx
	}
	ctx, cancel := context.WithCancel(b.ctx)
	p := &Page{id: id, ctx: ctx, cancel: cancel, log: b.log, browser: b}
	b.pages = append(b.pages, p)
	if b.current == nil {
		b.current = p
	}
	return p, ctx
}

func TestRemovePageLocked(t *testing.T) {
	t.Run("removes and reassigns current", func(t *testing.T) {
		b, _ := newTestBrowser(t)
		b.addTestPage(t, "a", true)
		b.addTestPage(t, "b", false)
		b.current = b.pages[1]

		removed := b.removePageLocked("b")

		if removed == nil || removed.id != "b" {
			t.Fatalf("removePageLocked returned %v, want page b", removed)
		}
		if len(b.pages) != 1 || b.pages[0].id != "a" {
			t.Fatalf("pages = %d entries, want only a", len(b.pages))
		}
		if b.current == nil || b.current.id != "a" {
			t.Errorf("current not reassigned to a surviving page: %v", b.current)
		}
	})

	t.Run("unknown id is a no-op", func(t *testing.T) {
		b, _ := newTestBrowser(t)
		b.addTestPage(t, "a", true)

		if removed := b.removePageLocked("nope"); removed != nil {
			t.Fatalf("removePageLocked(unknown) = %v, want nil", removed)
		}
		if len(b.pages) != 1 {
			t.Errorf("pages mutated on unknown id")
		}
	})

	t.Run("last page leaves current nil", func(t *testing.T) {
		b, _ := newTestBrowser(t)
		b.addTestPage(t, "a", false)

		b.removePageLocked("a")

		if b.current != nil {
			t.Errorf("current = %v, want nil after removing the last page", b.current)
		}
	})
}

func TestSetCurrentIfTracked(t *testing.T) {
	b, _ := newTestBrowser(t)
	b.addTestPage(t, "a", true)
	b.addTestPage(t, "b", false)

	if got := b.setCurrentIfTracked("b"); got == nil || b.current.id != "b" {
		t.Fatalf("setCurrentIfTracked(b) = %v, current = %v", got, b.current)
	}

	// A page removed between a caller's snapshot and its promotion must not
	// become current — its context is already cancelled.
	b.removePageLocked("b")
	if got := b.setCurrentIfTracked("b"); got != nil {
		t.Fatalf("setCurrentIfTracked(removed) = %v, want nil", got)
	}
	if b.current.id != "a" {
		t.Errorf("current = %v, want the still-tracked page a", b.current.id)
	}
}

func TestEvictDestroyedTarget(t *testing.T) {
	t.Run("non-primary is removed and cancelled", func(t *testing.T) {
		b, bctx := newTestBrowser(t)
		b.addTestPage(t, "primary", true)
		_, tabCtx := b.addTestPage(t, "tab", false)

		b.evictDestroyedTarget("tab")

		if len(b.pages) != 1 {
			t.Fatalf("destroyed tab still tracked: %d pages", len(b.pages))
		}
		if tabCtx.Err() == nil {
			t.Error("destroyed tab context not cancelled — chromedp target goroutines leak")
		}
		if bctx.Err() != nil {
			t.Fatal("browser context cancelled by evicting one tab")
		}
	})

	t.Run("primary is removed but never cancelled", func(t *testing.T) {
		// Chrome keeps running while sibling tabs remain, so a destroyed
		// primary does NOT mean the browser is going away. Cancelling its
		// context here would kill every sibling — and in Connect mode, a
		// Chrome process chromekit does not own.
		b, bctx := newTestBrowser(t)
		b.addTestPage(t, "primary", true)
		_, siblingCtx := b.addTestPage(t, "sibling", false)
		b.current = b.pages[0]

		b.evictDestroyedTarget("primary")

		if bctx.Err() != nil {
			t.Fatal("browser context cancelled when the primary tab was destroyed externally")
		}
		if siblingCtx.Err() != nil {
			t.Fatal("sibling tab killed as collateral damage")
		}
		if len(b.pages) != 1 || b.pages[0].id != "sibling" {
			t.Fatalf("dead primary left in bookkeeping: %d pages", len(b.pages))
		}
		if b.current == nil || b.current.id != "sibling" {
			t.Errorf("current still points at the destroyed primary: %v", b.current)
		}
	})

	t.Run("untracked target is ignored", func(t *testing.T) {
		b, _ := newTestBrowser(t)
		b.addTestPage(t, "tab", false)

		b.evictDestroyedTarget("some-iframe-or-worker")

		if len(b.pages) != 1 {
			t.Errorf("unrelated target destroyed our page")
		}
	})
}

func TestPageClose_RemovesFromBrowser(t *testing.T) {
	b, _ := newTestBrowser(t)
	b.addTestPage(t, "primary", true)
	p, pctx := b.addTestPage(t, "tab", false)

	p.Close()

	if len(b.pages) != 1 {
		t.Fatalf("Page.Close() left the page in b.pages: %d entries", len(b.pages))
	}
	if pctx.Err() == nil {
		t.Error("Page.Close() did not cancel the page context")
	}
	p.Close() // idempotent
}

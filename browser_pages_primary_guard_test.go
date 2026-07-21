package chromekit

import (
	"context"
	"errors"
	"testing"
)

// The primary page shares b.ctx/b.cancel with the Browser (attachToTarget with
// primary=true). Cancelling it therefore tears down every tab, and on the
// Launch path kills the Chrome process. These tests lock the guard that keeps
// a single-tab close from destroying the whole browser.

func TestCanClosePage_Table(t *testing.T) {
	cases := []struct {
		name     string
		primary  bool
		attached bool
		npages   int
		want     error
	}{
		{"non-primary among many, launch", false, false, 3, nil},
		{"non-primary among many, connect", false, true, 3, nil},
		{"primary among many, launch", true, false, 3, ErrCannotClosePrimaryPage},
		{"primary among many, connect", true, true, 3, ErrCannotClosePrimaryPage},
		{"last page, launch owns process", false, false, 1, ErrCannotCloseLastPage},
		{"last page, connect detaches", false, true, 1, nil},
		{"last page primary, launch", true, false, 1, ErrCannotCloseLastPage},
		{"last page primary, connect", true, true, 1, ErrCannotClosePrimaryPage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := canClosePage(tc.primary, tc.attached, tc.npages)
			if !errors.Is(got, tc.want) {
				t.Fatalf("canClosePage(primary=%v, attached=%v, n=%d) = %v, want %v",
					tc.primary, tc.attached, tc.npages, got, tc.want)
			}
		})
	}
}

func TestClosePage_Primary_ReturnsTypedErrorAndKeepsBrowserAlive(t *testing.T) {
	bctx, bcancel := context.WithCancel(context.Background())
	primary := &Page{id: "primary", ctx: bctx, cancel: bcancel, primary: true}
	otherCtx, otherCancel := context.WithCancel(bctx)
	other := &Page{id: "other", ctx: otherCtx, cancel: otherCancel}
	b := &Browser{
		log:     noopLogger{},
		cfg:     defaultConfig(),
		ctx:     bctx,
		cancel:  bcancel,
		pages:   []*Page{primary, other},
		current: primary,
	}

	err := b.ClosePage("primary")

	if !errors.Is(err, ErrCannotClosePrimaryPage) {
		t.Fatalf("ClosePage(primary) err = %v, want ErrCannotClosePrimaryPage", err)
	}
	if len(b.pages) != 2 {
		t.Errorf("primary must NOT be removed when guard fires (len=%d, want 2)", len(b.pages))
	}
	if bctx.Err() != nil {
		t.Fatalf("browser context was cancelled by ClosePage(primary) — every tab would die")
	}
	if otherCtx.Err() != nil {
		t.Errorf("sibling page context cancelled as collateral damage")
	}
}

func TestClosePage_NonPrimary_StillClosesAndRemoves(t *testing.T) {
	bctx, bcancel := context.WithCancel(context.Background())
	defer bcancel()
	primary := &Page{id: "primary", ctx: bctx, cancel: bcancel, primary: true}
	otherCtx, otherCancel := context.WithCancel(bctx)
	other := &Page{id: "other", ctx: otherCtx, cancel: otherCancel}
	b := &Browser{
		log:     noopLogger{},
		cfg:     defaultConfig(),
		ctx:     bctx,
		cancel:  bcancel,
		pages:   []*Page{primary, other},
		current: other,
	}

	if err := b.ClosePage("other"); err != nil {
		t.Fatalf("ClosePage(non-primary) err = %v, want nil", err)
	}
	if len(b.pages) != 1 || b.pages[0].id != "primary" {
		t.Fatalf("non-primary page not removed: %d pages left", len(b.pages))
	}
	if otherCtx.Err() == nil {
		t.Error("closed page context should be cancelled")
	}
	if bctx.Err() != nil {
		t.Fatal("browser context must survive a non-primary close")
	}
	if b.current != b.pages[0] {
		t.Error("current should fall back to a surviving page")
	}
}

func TestPageClose_Primary_IsNoOp(t *testing.T) {
	bctx, bcancel := context.WithCancel(context.Background())
	defer bcancel()
	p := &Page{id: "primary", ctx: bctx, cancel: bcancel, primary: true, log: noopLogger{}}

	p.Close()

	if bctx.Err() != nil {
		t.Fatal("Page.Close() on the primary page cancelled the browser context")
	}
}

func TestPageClose_NonPrimary_CancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &Page{id: "tab", ctx: ctx, cancel: cancel, log: noopLogger{}}

	p.Close()

	if ctx.Err() == nil {
		t.Fatal("Page.Close() on a normal page must cancel its context")
	}
}

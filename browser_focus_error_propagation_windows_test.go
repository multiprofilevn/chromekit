//go:build windows

package chromekit

import (
	"context"
	"errors"
	"testing"
)

// Phase 03 — Phase 02 F-TS4: the red-team catch.
//
// The bug recurred 3x partly because Windows Browser.Focus() called
// p.BringToFront() and on error did `b.log.Warnf(...)` then CONTINUED to
// SetForegroundWindow + native SendInput — firing input on an unconfirmed
// tab. Phase 02 F5 changes that to `return err`. This test locks the
// propagation contract: Focus must surface BringToFront's error, never
// swallow it.
//
// D3 — this file is //go:build windows. On a Linux-only CI it is silently
// skipped, so it is the SUPPLEMENTARY guard; the cross-platform primary gate
// is TestAwaitTabVisible_NeverVisible_DeterministicTimeout (B-TS2). The
// Windows-CI gap is a tracked follow-up in plan.md, not assumed-covered.
//
// Deterministic without Chrome: a canceled page ctx makes p.alive() return
// ErrContextCanceled, so BringToFront() errors before any chromedp.Run. On
// the CURRENT (pre-fix) code Focus() warns-and-continues → this test is RED.
// After Phase 02 (return err) → green.
//
// FLAG TO /cook (Phase 02): asserting lookupHWNDWithRetry/SetForegroundWindow
// were "not reached" needs a call-order spy. Per Phase 03 ownership we do NOT
// add production hooks here — the propagation contract (Focus returns
// BringToFront's exact error, never swallows) is proven via error identity.
// If Phase 02 later exposes a seam to force ErrTabActivateTimeout
// specifically, tighten the assertion to errors.Is(ErrTabActivateTimeout).
func TestFocus_PropagatesBringToFrontError_NoWarnAndContinue(t *testing.T) {
	// Arrange — page whose ctx is already canceled → BringToFront() fails
	// deterministically with ErrContextCanceled, no live Chrome needed.
	pctx, pcancel := context.WithCancel(context.Background())
	pcancel()
	p := &Page{id: "control", ctx: pctx, cancel: pcancel}
	b := &Browser{
		log:     noopLogger{},
		cfg:     defaultConfig(),
		pages:   []*Page{p},
		current: p,
	}

	// Act
	err := b.Focus(context.Background())

	// Assert — Focus must return the BringToFront error, not swallow it.
	if err == nil {
		t.Fatal("Focus() = nil — BringToFront error was swallowed (warn-and-continue: the red-team catch)")
	}
	if !errors.Is(err, ErrContextCanceled) {
		t.Fatalf("Focus() err = %v, want errors.Is ErrContextCanceled (the exact BringToFront error, propagated)", err)
	}
}

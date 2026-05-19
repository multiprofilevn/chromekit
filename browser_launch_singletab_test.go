package chromekit

import (
	"context"
	"errors"
	"testing"

	"github.com/chromedp/chromedp"
)

// Phase 03 — Phase 01 coverage (A: single-tab Launch via shared attach helper).
// TDD-RED: attachToTarget, resolveLaunchTarget, ErrNoLaunchTarget,
// ErrCannotCloseLastPage do not exist yet. The whole chromekit test package
// will fail to build until /cook lands Phase 00 + Phase 01 — that build
// failure IS the verified Red state for these specs.

// L1 — nil-safety: the shared attach helper must produce a fully wired Page.
// Mirrors the wirePage invariant guarded by TestAttachKnownTargets_WiresEveryPage.
// Pattern B: chromedp parent ctx, NO chromedp.Run (no live Chrome).
func TestAttachToTarget_WiresPageAndSetsCurrent(t *testing.T) {
	// Arrange
	parent, parentCancel := chromedp.NewContext(context.Background())
	defer parentCancel()
	b := &Browser{
		ctx:    parent,
		cancel: parentCancel,
		log:    noopLogger{},
		cfg:    defaultConfig(),
	}

	// Act — caller holds pagesMu (Phase 01 lock contract: helper does NOT lock).
	b.pagesMu.Lock()
	p, err := b.attachToTarget("primary", true /*primary*/)
	b.pagesMu.Unlock()

	// Assert
	if err != nil {
		t.Fatalf("attachToTarget(primary,true) err = %v, want nil", err)
	}
	if p == nil {
		t.Fatal("attachToTarget returned nil *Page")
	}
	if p.Mouse() == nil || p.Keyboard() == nil || p.Cookies() == nil || p.Downloads() == nil {
		t.Error("namespace API holder nil after attachToTarget — wirePage missed")
	}
	if len(b.pages) != 1 || b.pages[0] != p {
		t.Errorf("page not appended to b.pages (len=%d)", len(b.pages))
	}
	if b.current != p {
		t.Error("primary attach must make the page current (b.current != p)")
	}
}

// L2 — defensive nil-Target guard (R2: deterministic FromContext.Target
// acquisition, replaces the old 5s GetTargets poll). When chromedp bound no
// page target, Launch must fail loud with ErrNoLaunchTarget — NOT spawn a tab
// (re-introduces the 2-tab bug) and NOT enumerate (re-introduces the
// extension-welcome-tab steal race).
//
// FLAG TO /cook (Phase 01): the guard lives inside Launch(), which needs a
// real Chrome. For this to be unit-testable without a live browser, Phase 01
// MUST extract the predicate as `resolveLaunchTarget(ctx) (string, error)`
// (mirrors Phase 02's awaitTabVisible injectable-seam philosophy). Do NOT edit
// Phase 01 source from Phase 03 — if the seam is absent, this stays Red and
// the gap is documented in the test report.
func TestResolveLaunchTarget_NilContextFailsLoudNoSpawn(t *testing.T) {
	// Arrange — a plain context carries no chromedp Context value, so
	// chromedp.FromContext(ctx) == nil → the c==nil branch.
	bare := context.Background()

	// Act
	tid, err := resolveLaunchTarget(bare)

	// Assert — fail loud, no fallback target id.
	if !errors.Is(err, ErrNoLaunchTarget) {
		t.Fatalf("resolveLaunchTarget(bare) err = %v, want errors.Is ErrNoLaunchTarget", err)
	}
	if tid != "" {
		t.Errorf("on nil-Target guard tid = %q, want empty (no spawn / no enumerate fallback)", tid)
	}
}

// L4(a) — F-NEW-2 guard: on the Launch owns-process path (attached==false),
// closing the only page must NOT silently let Chrome die. Return the typed
// ErrCannotCloseLastPage so the caller explicitly chooses Close() vs keep.
func TestClosePage_LastPageOwnsProcess_ReturnsTypedError(t *testing.T) {
	// Arrange
	pctx, pcancel := context.WithCancel(context.Background())
	p := &Page{id: "only", ctx: pctx, cancel: pcancel}
	b := &Browser{
		log:      noopLogger{},
		cfg:      defaultConfig(),
		attached: false, // Launch owns the process
		pages:    []*Page{p},
		current:  p,
	}

	// Act
	err := b.ClosePage("only")

	// Assert
	if !errors.Is(err, ErrCannotCloseLastPage) {
		t.Fatalf("ClosePage(last, attached=false) err = %v, want ErrCannotCloseLastPage", err)
	}
	if len(b.pages) != 1 {
		t.Errorf("page must NOT be removed when guard fires (len=%d, want 1)", len(b.pages))
	}
}

// L4(b) — Connect detach semantics preserved (historical regression class:
// 260509-0833-safe-remote-allocator). attached==true single page must NOT
// trip the guard.
func TestClosePage_LastPageAttached_DoesNotTripGuard(t *testing.T) {
	// Arrange
	pctx, pcancel := context.WithCancel(context.Background())
	p := &Page{id: "only", ctx: pctx, cancel: pcancel}
	b := &Browser{
		log:      noopLogger{},
		cfg:      defaultConfig(),
		attached: true, // Connect — user owns the process
		pages:    []*Page{p},
		current:  p,
	}

	// Act
	err := b.ClosePage("only")

	// Assert — must not be the guard error (current behavior: nil + removed).
	if errors.Is(err, ErrCannotCloseLastPage) {
		t.Fatalf("ClosePage(last, attached=true) returned ErrCannotCloseLastPage — Connect detach semantics broken")
	}
}

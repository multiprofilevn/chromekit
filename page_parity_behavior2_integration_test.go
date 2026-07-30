//go:build integration

// Behavior tests for phase-06's new Page methods (chromekit superset
// parity): Focus/Hover/ClickAt, Wait/EvaluateCtx, AllFrames — continuation
// of page_parity_behavior_integration_test.go (Test Spec items 7-9).
package chromekit

import (
	"context"
	"errors"
	"testing"
	"time"
)

// No literal '#' in this HTML: inside a data:text/html, URL an unencoded '#'
// starts a URL fragment and truncates everything after it (bit both the
// stylesheet and the button/script that followed a `#btn` selector). Use
// attribute selectors instead of id selectors to stay '#'-free.
const focusHoverPage = `data:text/html,<body>` +
	`<style>[id=btn]:hover{background:red}</style>` +
	`<input id="inp">` +
	`<button id="btn" style="position:absolute;left:50px;top:50px;width:80px;height:30px">Go</button>` +
	`<script>window.__clicked = false; document.getElementById('btn').addEventListener('click', () => { window.__clicked = true; });</script>` +
	`</body>`

// 7. Focus focuses the right element and silently no-ops on a missing
// selector (matches firefoxkit's own Focus, which routes through
// EvalSelector with out=nil — a null/no-match result never surfaces).
// Hover triggers :hover. ClickAt lands on the element under the point.
func TestIntegration_Page_FocusHoverClickAt(t *testing.T) {
	p := bootIntegrationPage(t, focusHoverPage)

	if err := p.Focus("#inp"); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	var activeID string
	if err := p.Evaluate(`document.activeElement && document.activeElement.id`, &activeID); err != nil {
		t.Fatalf("read activeElement: %v", err)
	}
	if activeID != "inp" {
		t.Errorf("document.activeElement.id = %q, want %q", activeID, "inp")
	}

	if err := p.Focus("#nope"); err != nil {
		t.Errorf("Focus on missing selector should not error (firefoxkit parity), got %v", err)
	}

	if err := p.Hover("#btn"); err != nil {
		t.Fatalf("Hover: %v", err)
	}
	var hovered bool
	if err := p.Evaluate(`document.querySelector('#btn:hover') != null`, &hovered); err != nil {
		t.Fatalf("read :hover: %v", err)
	}
	if !hovered {
		t.Errorf("Hover did not trigger :hover on #btn")
	}

	// #btn spans (50,50)-(130,80) in viewport CSS pixels; (90,65) is its center.
	if err := p.ClickAt(90, 65); err != nil {
		t.Fatalf("ClickAt: %v", err)
	}
	var clicked bool
	if err := p.Evaluate(`window.__clicked`, &clicked); err != nil {
		t.Fatalf("read __clicked: %v", err)
	}
	if !clicked {
		t.Errorf("ClickAt(90, 65) did not land on #btn")
	}
}

// 8. Wait blocks for (at least) its duration and returns an error once the
// page context is cancelled (Browser.Close) instead of waiting it out.
// EvaluateCtx honors an externally supplied, already-cancelled context.
func TestIntegration_Page_WaitAndEvaluateCtx(t *testing.T) {
	p := bootIntegrationPage(t, `data:text/html,<body>wait</body>`)

	start := time.Now()
	if err := p.Wait(150 * time.Millisecond); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Errorf("Wait returned after %s, want >= 150ms", elapsed)
	}

	// Second browser: cancel its page context mid-Wait via Close, so Wait
	// must return early instead of blocking the full 5s.
	chromePath := resolveChromePath(t)
	b2, err := Launch(chromePath, WithHeadless(true), WithCursorProfile(CursorMachine))
	if err != nil {
		t.Fatalf("launch second browser: %v", err)
	}
	p2 := b2.Current()
	done := make(chan error, 1)
	go func() { done <- p2.Wait(5 * time.Second) }()
	time.Sleep(150 * time.Millisecond)
	_ = b2.Close()
	select {
	case werr := <-done:
		if werr == nil {
			t.Errorf("Wait should report an error once the page context is cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not observe context cancellation in time")
	}

	cctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call
	var out string
	if err := p.EvaluateCtx(cctx, `1+1`, &out); err == nil {
		t.Fatal("EvaluateCtx with an already-cancelled ctx should error")
	} else if !errors.Is(err, ErrContextCanceled) {
		t.Errorf("EvaluateCtx err = %v, want wraps ErrContextCanceled", err)
	}
}

const allFramesPage = `data:text/html,<body>` +
	`<iframe id="a" srcdoc="<body>outer1<iframe id='b' srcdoc='<body>inner</body>'></iframe></body>"></iframe>` +
	`<iframe id="c" srcdoc="<body>outer2</body>"></iframe>` +
	`</body>`

// 9. AllFrames returns every descendant frame flattened — 2 top-level
// iframes plus 1 nested inside the first — with the main page's own frame
// excluded (unlike Frames, which includes it).
func TestIntegration_Page_AllFrames(t *testing.T) {
	p := bootIntegrationPage(t, allFramesPage)
	time.Sleep(300 * time.Millisecond) // let the iframes attach

	frames, err := p.AllFrames(context.Background())
	if err != nil {
		t.Fatalf("AllFrames: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("AllFrames returned %d frame(s), want 3 (2 top iframes + 1 nested)", len(frames))
	}
}

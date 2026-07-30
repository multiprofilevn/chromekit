//go:build integration

// Behavior tests for phase-06's MouseAPI.MiddleClick and Browser.Close()
// error — see phase-06-chromekit-superset-parity.md, Test Spec items 10-11.
package chromekit

import "testing"

const middleClickPage = `data:text/html,<body>` +
	`<script>` +
	`window.__auxButton = -1;` +
	`document.addEventListener('auxclick', e => { window.__auxButton = e.button; });` +
	`</script>` +
	`</body>`

// 10. MiddleClick dispatches the middle button — browsers report middle
// clicks via the auxclick event (not click), with button === 1.
func TestIntegration_Mouse_MiddleClick(t *testing.T) {
	p := bootIntegrationPage(t, middleClickPage)
	if err := p.Mouse().MiddleClick(100, 100); err != nil {
		t.Fatalf("MiddleClick: %v", err)
	}
	var btn float64
	if err := p.Evaluate(`window.__auxButton`, &btn); err != nil {
		t.Fatalf("read __auxButton: %v", err)
	}
	if int(btn) != 1 {
		t.Errorf("auxclick button = %v, want 1 (middle)", btn)
	}
}

// 11. Browser.Close() returns error. Pinned contract (documented on
// Browser.Close, matches firefoxkit's own second-call behavior): every
// call, first or repeated, returns nil — chromekit's teardown is pure
// context cancellation with no failure path to surface.
func TestIntegration_Browser_CloseReturnsError(t *testing.T) {
	chromePath := resolveChromePath(t)
	b, err := Launch(chromePath, WithHeadless(true))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("first Close() = %v, want nil", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
}

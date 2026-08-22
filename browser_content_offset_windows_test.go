//go:build windows

package chromekit

import (
	"testing"

	"github.com/multiprofilevn/chromekit/input"
)

// Chrome paints its own title bar and tab strip INSIDE the Win32 client area,
// so GetClientRect cannot say where the content starts and the measurement has
// to come from the renderer. That makes it untrusted input: window.screenX,
// outerWidth and innerWidth are all properties of the page's own window object,
// and page script can redefine every one of them.
//
// The consequence if the number is used unbounded is not a mis-click inside the
// page. The native backend drives the PHYSICAL cursor, so a page that inflates
// screenX steers the real pointer out of the browser and fires a real click
// wherever it lands — a taskbar item, a "Yes" on someone's UAC-style dialog,
// another profile's window. GetWindowRect is the bound, because it answers to
// the window manager and nothing inside the page can reach it.

// newOffsetTestBrowser builds a Browser whose two measurement sources are both
// fixtures: the page-reported metrics and the out-of-band window rectangle.
func newOffsetTestBrowser(t *testing.T, declaredX, declaredY int, m windowMetrics, r input.Rect) *Browser {
	t.Helper()
	prevMetrics, prevRect := pageWindowMetrics, browserWindowRect
	t.Cleanup(func() { pageWindowMetrics, browserWindowRect = prevMetrics, prevRect })

	cfg := defaultConfig()
	WithNativeWindow(declaredX, declaredY, 1.0)(cfg)

	pageWindowMetrics = func(*Browser) (windowMetrics, error) { return m, nil }
	browserWindowRect = func(*Browser) (input.Rect, error) { return r, nil }

	return &Browser{log: noopLogger{}, cfg: cfg}
}

// 1. TestContentOffset_IsAbsolute: the return value is the absolute screen
// position of CSS (0,0), so it cannot depend on the window position the caller
// happened to declare via WithNativeWindow.
//
// Today it subtracts that declared position and returns a RELATIVE offset,
// while firefoxkit's identically-named method returns an absolute one. Same
// signature, two meanings — the fleet adds one to a window origin and the other
// to nothing, and only one of those is right.
func TestContentOffset_IsAbsolute(t *testing.T) {
	// One physical window at screen (1000, 400); the renderer reports the same
	// metrics either way, because where the window actually is has nothing to do
	// with what the caller declared.
	metrics := windowMetrics{
		ScreenX: 1000, ScreenY: 400,
		OuterWidth: 800, OuterHeight: 600,
		InnerWidth: 784, InnerHeight: 520,
		DevicePixelRatio: 1,
	}
	rect := input.Rect{Left: 1000, Top: 400, Right: 1800, Bottom: 1000}
	// border = (800-784)/2 = 8; chromeY = (600-520) - 8 = 72, because the 8px
	// bottom border is part of outerHeight-innerHeight but sits UNDER the
	// content, not above it.
	const wantX, wantY = 1008, 472

	b1 := newOffsetTestBrowser(t, 0, 0, metrics, rect)
	got1X, got1Y, err := b1.ContentOffset()
	if err != nil {
		t.Fatalf("ContentOffset (declared 0,0): %v", err)
	}

	b2 := newOffsetTestBrowser(t, 1000, 400, metrics, rect)
	got2X, got2Y, err := b2.ContentOffset()
	if err != nil {
		t.Fatalf("ContentOffset (declared 1000,400): %v", err)
	}

	if got1X != got2X || got1Y != got2Y {
		t.Fatalf("ContentOffset depends on the declared window position: (%d,%d) vs (%d,%d) — it must be absolute",
			got1X, got1Y, got2X, got2Y)
	}
	if got1X != wantX || got1Y != wantY {
		t.Fatalf("ContentOffset = (%d,%d), want the absolute content origin (%d,%d)",
			got1X, got1Y, wantX, wantY)
	}
}

// 10. TestContentOffset_RejectsSpoofedWindowMetrics: page script inflates
// screenX by ~900px; the window manager says the window is nowhere near there.
// ContentOffset must FAIL rather than hand a cursor-steering number to the
// native backend. Failing the job is the correct outcome — the alternative is a
// real click at coordinates an attacker chose.
//
// This is a security control. Anyone tempted to drop it "to simplify" should
// note that the check is the only thing standing between page JavaScript and
// the physical mouse.
func TestContentOffset_RejectsSpoofedWindowMetrics(t *testing.T) {
	spoofed := windowMetrics{
		ScreenX: 1700, ScreenY: 1200, // window really sits at (0,0)
		OuterWidth: 800, OuterHeight: 600,
		InnerWidth: 784, InnerHeight: 520,
		DevicePixelRatio: 1,
	}
	realRect := input.Rect{Left: 0, Top: 0, Right: 800, Bottom: 600}

	b := newOffsetTestBrowser(t, 0, 0, spoofed, realRect)

	x, y, err := b.ContentOffset()
	if err == nil {
		t.Fatalf("ContentOffset accepted a spoofed content origin (%d,%d) that sits outside the window rect %+v — "+
			"page script can now steer the physical cursor anywhere on the desktop", x, y, realRect)
	}
}

// 11. TestContentOffset_AcceptsWithinTolerance: the bound has to tolerate the
// couple of pixels that rounding and window borders legitimately produce. A
// clamp that rejects those fails every real job and gets switched off, which
// costs more than it saves.
func TestContentOffset_AcceptsWithinTolerance(t *testing.T) {
	metrics := windowMetrics{
		ScreenX: 0, ScreenY: 0,
		OuterWidth: 800, OuterHeight: 600,
		InnerWidth: 784, InnerHeight: 520,
		DevicePixelRatio: 1,
	}
	// Content origin lands at (8, 72); the rect starts 2px to the right of it.
	rect := input.Rect{Left: 10, Top: 0, Right: 810, Bottom: 600}

	b := newOffsetTestBrowser(t, 0, 0, metrics, rect)

	gotX, gotY, err := b.ContentOffset()
	if err != nil {
		t.Fatalf("ContentOffset rejected a 2px overhang as spoofing: %v", err)
	}
	if gotX != 8 || gotY != 72 {
		t.Fatalf("ContentOffset = (%d,%d), want (8,72)", gotX, gotY)
	}
}

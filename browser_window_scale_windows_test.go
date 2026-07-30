//go:build windows

package chromekit

import (
	"strings"
	"testing"

	"github.com/tuwibu/chromekit/input"
)

// The numbers in this file are not invented. They come from a real Chrome
// window launched with --force-device-scale-factor=0.3333 and
// --window-size=1920,1080, read back in the same breath from the page realm and
// from GetWindowRect:
//
//	dpr=0.333333  outer=1041x798  inner=993x681  screen=(2358,1161)
//	GetWindowRect=(786,387)-(1133,653)  →  347x266 physical
//	GetDpiForWindow/96 = 1.0
//
// Every one of those CSS lengths is exactly three times its physical twin, and
// the OS DPI ratio — the number the kit used to hand the native backend — is
// off by a factor of three. That is the whole bug: a CSS x of ~917 multiplied
// by 1.0 instead of 0.3333 aimed the physical cursor at x=2197 on a 1920-wide
// screen.

const oneThird = 1.0 / 3.0

// realForcedScaleMetrics is the page-side half of the measurement above.
func realForcedScaleMetrics() windowMetrics {
	return windowMetrics{
		ScreenX: 2358, ScreenY: 1161,
		OuterWidth: 1041, OuterHeight: 798,
		InnerWidth: 993, InnerHeight: 681,
		DevicePixelRatio: oneThird,
	}
}

// realForcedScaleRect is the window-manager half of the same measurement.
func realForcedScaleRect() input.Rect {
	return input.Rect{Left: 786, Top: 387, Right: 1133, Bottom: 653}
}

// captureWarn collects warning text so a test can assert the fallback was
// announced rather than taken silently.
func captureWarn(dst *[]string) func(string, ...any) {
	return func(format string, _ ...any) { *dst = append(*dst, format) }
}

// A forced device scale factor is invisible to GetDpiForWindow, so the OS DPI
// ratio says 1.0 while the window is really rendering at a third. The page's
// own devicePixelRatio is the only source that knows, and the window rectangle
// confirms it independently.
func TestResolveWindowScale_PrefersForcedFactorOverOSDPI(t *testing.T) {
	var warns []string
	got := resolveWindowScale(realForcedScaleMetrics(), realForcedScaleRect(), 1.0, captureWarn(&warns))
	if got != oneThird {
		t.Fatalf("resolveWindowScale = %g, want %g — the forced device scale factor was ignored", got, oneThird)
	}
	if len(warns) != 0 {
		t.Fatalf("agreeing measurements should not warn, got %v", warns)
	}
}

// Without a forced factor the two sources agree on the OS DPI ratio anyway, so
// nothing changes for the ordinary case.
func TestResolveWindowScale_UnforcedWindowKeepsOSDPIRatio(t *testing.T) {
	// Real unforced measurement: outer 1920x1080, rect (640,360)-(2560,1440).
	m := windowMetrics{
		ScreenX: 640, ScreenY: 360,
		OuterWidth: 1920, OuterHeight: 1080,
		InnerWidth: 1904, InnerHeight: 929,
		DevicePixelRatio: 1,
	}
	r := input.Rect{Left: 640, Top: 360, Right: 2560, Bottom: 1440}
	var warns []string
	if got := resolveWindowScale(m, r, 1.0, captureWarn(&warns)); got != 1.0 {
		t.Fatalf("resolveWindowScale = %g, want 1.0", got)
	}
	if len(warns) != 0 {
		t.Fatalf("agreeing measurements should not warn, got %v", warns)
	}
}

// The security case. devicePixelRatio is read from the page realm and script can
// redefine it; a large one multiplies every CSS coordinate on its way to the
// PHYSICAL cursor, so believing it unchecked hands page JavaScript a lever on
// the real mouse. GetWindowRect disagrees, so the trusted OS ratio wins.
//
// Anyone tempted to drop the cross-check "because devicePixelRatio is the
// definition of the ratio" should note that being the right definition and
// being an honest number are different properties.
func TestResolveWindowScale_RefusesSpoofedDevicePixelRatio(t *testing.T) {
	m := realForcedScaleMetrics()
	m.DevicePixelRatio = 4.0 // window really renders at 1/3

	var warns []string
	got := resolveWindowScale(m, realForcedScaleRect(), 1.0, captureWarn(&warns))
	if got != 1.0 {
		t.Fatalf("resolveWindowScale = %g, want the OS DPI ratio 1.0 — a spoofed devicePixelRatio was believed", got)
	}
	if len(warns) == 0 {
		t.Fatal("fallback to the OS DPI ratio must be logged, not taken silently")
	}
	if !strings.Contains(warns[0], "disagrees") {
		t.Fatalf("warning %q should name the disagreement", warns[0])
	}
}

// A page that lies about its size to make a lie about the ratio self-consistent
// still cannot win: shrinking outerWidth to match a spoofed dpr changes the
// estimate too, and the two only agree when the page told the truth. This case
// pins the opposite direction — outer size spoofed, dpr honest.
func TestResolveWindowScale_RefusesSpoofedOuterSize(t *testing.T) {
	m := realForcedScaleMetrics()
	m.OuterWidth = 10 // would make the estimate 34.7 instead of 0.333

	var warns []string
	if got := resolveWindowScale(m, realForcedScaleRect(), 1.0, captureWarn(&warns)); got != 1.0 {
		t.Fatalf("resolveWindowScale = %g, want the OS DPI ratio 1.0", got)
	}
	if len(warns) == 0 {
		t.Fatal("fallback must be logged")
	}
}

// Tolerance band. The only deviation a healthy window produces is the browser
// rounding outerWidth to a whole CSS pixel; anything approaching a whole factor
// is a different window being described.
func TestResolveWindowScale_ToleranceBand(t *testing.T) {
	rect := input.Rect{Left: 0, Top: 0, Right: 1000, Bottom: 500}
	cases := []struct {
		name string
		dpr  float64
		want float64
	}{
		{"exact", 1.0, 1.0},
		{"within tolerance low", 0.99, 0.99},
		{"within tolerance high", 1.01, 1.01},
		{"just inside the tolerance edge", 1.019, 1.019},
		{"just past the edge", 1.021, 2.5},
		{"whole factor off", 3.0, 2.5},
		{"absurdly large", 1e6, 2.5},
		{"zero — no measurement", 0, 2.5},
		{"negative", -1, 2.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := windowMetrics{OuterWidth: 1000, OuterHeight: 500, DevicePixelRatio: tc.dpr}
			var warns []string
			// osScale is a value no honest path produces, so a pass cannot be
			// confused with the page value being echoed back.
			if got := resolveWindowScale(m, rect, 2.5, captureWarn(&warns)); got != tc.want {
				t.Fatalf("resolveWindowScale(dpr=%g) = %g, want %g", tc.dpr, got, tc.want)
			}
		})
	}
}

// A rectangle with no area cannot estimate anything, so the page value is not
// checkable and the trusted ratio is used.
func TestResolveWindowScale_DegenerateRect(t *testing.T) {
	m := realForcedScaleMetrics()
	var warns []string
	if got := resolveWindowScale(m, input.Rect{}, 1.25, captureWarn(&warns)); got != 1.25 {
		t.Fatalf("resolveWindowScale = %g, want the OS DPI ratio 1.25", got)
	}
	if len(warns) == 0 {
		t.Fatal("an uncheckable page value must be logged when it is discarded")
	}
}

// outerHeight-innerHeight is the chrome above the content PLUS the resize
// border below it. Adding the whole thing puts the origin a border's worth too
// low, and every click lands that far under its target.
//
// Numbers from a real tile of bing_chrome_native: window 640x360 physical at
// (0,0), forced scale 1/3, page reporting outer 1920x1080 and inner 1896x981.
// A click recorder installed in the page caught the consequence — aiming at a
// result link whose box was y ∈ [245.8, 266.8] delivered the click at y=267,
// one pixel past the bottom edge, onto the enclosing <h2>; 6 of 9 instances
// failed to navigate. The border is (1920-1896)/2 = 12 CSS = 4 physical px,
// which is exactly the miss.
func TestContentOffset_ExcludesTheBottomResizeBorder(t *testing.T) {
	prev := osDPIScale
	t.Cleanup(func() { osDPIScale = prev })
	osDPIScale = func(*Browser) float64 { return 1.0 }

	m := windowMetrics{
		ScreenX: 0, ScreenY: 0,
		OuterWidth: 1920, OuterHeight: 1080,
		InnerWidth: 1896, InnerHeight: 981,
		DevicePixelRatio: oneThird,
	}
	rect := input.Rect{Left: 0, Top: 0, Right: 640, Bottom: 360}

	b := newOffsetTestBrowser(t, 0, 0, m, rect)
	gotX, gotY, err := b.ContentOffset()
	if err != nil {
		t.Fatalf("ContentOffset: %v", err)
	}
	// border = 12 CSS = 4 physical; chromeY = (1080-981) - 12 = 87 CSS = 29.
	if gotX != 4 || gotY != 29 {
		t.Fatalf("ContentOffset = (%d,%d), want (4,29) — 33 down the y axis means the "+
			"bottom border was counted as chrome above the content", gotX, gotY)
	}
}

// ContentOffset reports a SCREEN position, so the CSS lengths the renderer
// hands back have to be converted before they are compared with — or bounded
// by — anything the window manager said. Before this, the raw CSS origin
// (2382,1278) was compared against a rect ending at x=1133 and the whole
// measurement was refused, taking the native path down with it.
func TestContentOffset_ScalesPageMetricsToPhysicalPixels(t *testing.T) {
	prev := osDPIScale
	t.Cleanup(func() { osDPIScale = prev })
	osDPIScale = func(*Browser) float64 { return 1.0 } // forced factor is invisible to it

	b := newOffsetTestBrowser(t, 0, 0, realForcedScaleMetrics(), realForcedScaleRect())

	gotX, gotY, err := b.ContentOffset()
	if err != nil {
		t.Fatalf("ContentOffset refused a scaled window: %v", err)
	}
	// border = (1041-993)/2 = 24 CSS; chromeY = (798-681) - 24 = 93.
	// (2358 + 24) / 3 = 794 ; (1161 + 93) / 3 = 418 — both inside the rect.
	if gotX != 794 || gotY != 418 {
		t.Fatalf("ContentOffset = (%d,%d), want (794,418) in physical screen pixels", gotX, gotY)
	}
}

//go:build windows

package input

import (
	"math"
	"testing"
)

// TestCSSScreenInverse asserts cssToScreen / screenToCSS round-trip exactly
// for any (x, y) under any Window config. Regression for the
// "teleport then glide" cursor artefact: chromefleet's clickAt does
// MoveTo(target) → ClickAt(target). ClickAt internally re-runs MoveTo,
// which seeds its Bezier start from OSPositionCSS. If screenToCSS is NOT
// the inverse of cssToScreen, the seed disagrees with reality by
// ~ContentOffset/Scale px; the next backend.MouseMove call jumps the
// cursor by exactly that error, then animates back to target.
func TestCSSScreenInverse(t *testing.T) {
	cases := []struct {
		name string
		win  Window
	}{
		{"origin, scale=1, no chrome offset", Window{X: 0, Y: 0, Scale: 1.0}},
		{"offset window, scale=1, no chrome offset", Window{X: 100, Y: 200, Scale: 1.0}},
		{"with chrome offset, scale=1", Window{X: 100, Y: 200, ContentOffsetX: 8, ContentOffsetY: 87, Scale: 1.0}},
		{"with chrome offset, scale=1.5", Window{X: 100, Y: 200, ContentOffsetX: 8, ContentOffsetY: 87, Scale: 1.5}},
		{"second monitor, scale=1.25", Window{X: 1920, Y: 0, ContentOffsetX: 8, ContentOffsetY: 87, Scale: 1.25}},
		{"zero scale defaults to 1", Window{X: 50, Y: 50, ContentOffsetX: 8, ContentOffsetY: 87, Scale: 0}},
	}
	points := [][2]float64{
		{0, 0}, {1, 1}, {100, 200}, {500.5, 300.25}, {1280, 720},
	}
	for _, tc := range cases {
		for _, p := range points {
			sx, sy := cssToScreen(p[0], p[1], tc.win)
			gx, gy := screenToCSS(sx, sy, tc.win)
			if math.Abs(gx-p[0]) > 1e-9 || math.Abs(gy-p[1]) > 1e-9 {
				t.Errorf("%s: round-trip (%.2f,%.2f) → screen (%.2f,%.2f) → CSS (%.6f,%.6f), drift x=%.6g y=%.6g",
					tc.name, p[0], p[1], sx, sy, gx, gy, gx-p[0], gy-p[1])
			}
		}
	}
}

// TestScreenToCSS_AccountsForContentOffset is the explicit assertion that
// screenToCSS subtracts ContentOffset (the v0.4.0 bug forgot this).
// A point at the exact content origin (winX+offX, winY+offY) must map to
// CSS (0, 0).
func TestScreenToCSS_AccountsForContentOffset(t *testing.T) {
	win := Window{X: 100, Y: 200, ContentOffsetX: 8, ContentOffsetY: 87, Scale: 1.0}
	cssX, cssY := screenToCSS(float64(win.X+win.ContentOffsetX), float64(win.Y+win.ContentOffsetY), win)
	if cssX != 0 || cssY != 0 {
		t.Errorf("content origin should map to CSS (0,0); got (%.3f, %.3f) — ContentOffset not subtracted",
			cssX, cssY)
	}
}

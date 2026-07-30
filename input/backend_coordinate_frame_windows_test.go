//go:build windows

package input

import (
	"context"
	"sync"
	"testing"
)

// The coordinate frame is a single struct that three things must agree on: the
// kit that emits the click, the inverse transform that seeds the next bezier,
// and the fleet's drift guard. These specs pin the frame's convention and the
// mechanism that updates it in place.

// ---------------------------------------------------------------- frame ----

// 2. TestCssToScreen_NoWindowOriginTerm pins the convention: ContentOffsetX/Y
// carry the ABSOLUTE screen position of CSS (0,0) and X/Y are zero, so there is
// no window-origin term left for a caller to add a second time. Double-counting
// the window position is the shape of B1/B3 — the click lands one whole window
// offset away, and the drift guard reports it as the human moving the mouse.
func TestCssToScreen_NoWindowOriginTerm(t *testing.T) {
	cases := []struct {
		name           string
		win            Window
		cssX, cssY     float64
		wantX, wantY   float64
	}{
		{"scale 1.25, the spec's worked example",
			Window{X: 0, Y: 0, ContentOffsetX: 1000, ContentOffsetY: 500, Scale: 1.25}, 100, 80, 1125, 600},
		{"scale 1.0, origin of the content area",
			Window{X: 0, Y: 0, ContentOffsetX: 8, ContentOffsetY: 87, Scale: 1.0}, 0, 0, 8, 87},
		{"scale 1.0, arbitrary point",
			Window{X: 0, Y: 0, ContentOffsetX: 8, ContentOffsetY: 87, Scale: 1.0}, 400, 600, 408, 687},
		{"scale 1.25, second monitor",
			Window{X: 0, Y: 0, ContentOffsetX: 1928, ContentOffsetY: 104, Scale: 1.25}, 400, 600, 2428, 854},
		{"scale 1.5, fractional css",
			Window{X: 0, Y: 0, ContentOffsetX: 100, ContentOffsetY: 200, Scale: 1.5}, 37.5, 21, 156.25, 231.5},
		{"scale 1.5, large viewport",
			Window{X: 0, Y: 0, ContentOffsetX: 1208, ContentOffsetY: 404, Scale: 1.5}, 1280, 720, 3128, 1484},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotX, gotY := cssToScreen(tc.cssX, tc.cssY, tc.win)
			if gotX != tc.wantX || gotY != tc.wantY {
				t.Fatalf("cssToScreen(%g,%g) = (%g,%g), want (%g,%g)",
					tc.cssX, tc.cssY, gotX, gotY, tc.wantX, tc.wantY)
			}
		})
	}
}

// 3. TestScreenToCSS_RoundTrip re-runs the existing inverse invariant against
// the absolute-offset data the rest of this phase produces. The transforms are
// deliberately NOT being changed; this is the tripwire that says so.
func TestScreenToCSS_RoundTrip(t *testing.T) {
	frames := []Window{
		{X: 0, Y: 0, ContentOffsetX: 1000, ContentOffsetY: 500, Scale: 1.25},
		{X: 0, Y: 0, ContentOffsetX: 1928, ContentOffsetY: 104, Scale: 1.5},
		{X: 0, Y: 0, ContentOffsetX: 8, ContentOffsetY: 87, Scale: 1.0},
	}
	points := [][2]float64{{0, 0}, {1, 1}, {400, 600}, {1279.5, 719.25}}
	for _, win := range frames {
		for _, p := range points {
			sx, sy := cssToScreen(p[0], p[1], win)
			cx, cy := screenToCSS(sx, sy, win)
			if cx != p[0] || cy != p[1] {
				t.Fatalf("frame %+v: css(%g,%g) → screen(%g,%g) → css(%g,%g)",
					win, p[0], p[1], sx, sy, cx, cy)
			}
		}
	}
}

// ------------------------------------------------------------ SetWindow ----

// 4. TestSetWindow_UpdatesInPlace: the frame changes, the object does not.
//
// The object identity half is the load-bearing half. Roughly thirty call sites
// read p.backend / p.cursor / p.keyboard bare, so a design that re-measures by
// building a NEW backend and reassigning the field can be observed by any of
// them at the wrong moment. Updating in place makes that class of bug
// unrepresentable.
func TestSetWindow_UpdatesInPlace(t *testing.T) {
	b := New(TypeNative, Config{Window: Window{ContentOffsetX: 100, ContentOffsetY: 200, Scale: 1.0}})
	before := b.(*nativeBackend)

	gotX, gotY := before.toScreen(10, 10)
	if gotX != 110 || gotY != 210 {
		t.Fatalf("initial frame: toScreen(10,10) = (%d,%d), want (110,210)", gotX, gotY)
	}

	b.SetWindow(Window{ContentOffsetX: 1000, ContentOffsetY: 500, Scale: 1.25})

	if after := b.(*nativeBackend); after != before {
		t.Fatal("SetWindow replaced the backend object; it must mutate the existing one")
	}
	gotX, gotY = before.toScreen(100, 80)
	if gotX != 1125 || gotY != 600 {
		t.Fatalf("after SetWindow: toScreen(100,80) = (%d,%d), want (1125,600)", gotX, gotY)
	}
}

// 5. TestSetWindow_DoesNotTearMultiStepOp is the stuck-mouse-button guard.
//
// SetWindow lands strictly between the MouseDown and the MouseUp of one click.
// Both halves must reach the same backend and the button MUST come back up. The
// failure this forbids is not a mis-click: swapping the backend pointer instead
// of mutating it leaves the OS holding the left button down with nobody left
// holding the object that would release it, and nothing short of a human click
// clears that — for every application on the machine, not just the browser.
//
// Run under -race -count=100.
func TestSetWindow_DoesNotTearMultiStepOp(t *testing.T) {
	origMouse, origCursor := sendMouseFn, setCursorPosFn
	t.Cleanup(func() { sendMouseFn, setCursorPosFn = origMouse, origCursor })

	var mu sync.Mutex
	var flags []uint32
	var framesAtEmit []Window

	frameA := Window{ContentOffsetX: 100, ContentOffsetY: 200, Scale: 1.0}
	frameB := Window{ContentOffsetX: 1000, ContentOffsetY: 500, Scale: 1.25}

	b := New(TypeNative, Config{Window: frameA})
	n := b.(*nativeBackend)

	downReached := make(chan struct{})
	setWindowDone := make(chan struct{})

	setCursorPosFn = func(int32, int32) {}
	sendMouseFn = func(f, _ uint32) error {
		mu.Lock()
		flags = append(flags, f)
		framesAtEmit = append(framesAtEmit, n.window())
		mu.Unlock()
		if f == mouseEventfLeftDown {
			// Hand over to the re-measure, then continue. Deterministic
			// interleaving — no sleep decides whether this test is meaningful.
			close(downReached)
			<-setWindowDone
		}
		return nil
	}

	go func() {
		<-downReached
		b.SetWindow(frameB)
		close(setWindowDone)
	}()

	ctx := context.Background()
	if err := n.MouseDown(ctx, 10, 10, MouseLeft); err != nil {
		t.Fatalf("MouseDown: %v", err)
	}
	if err := n.MouseUp(ctx, 10, 10, MouseLeft); err != nil {
		t.Fatalf("MouseUp: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(flags) != 2 || flags[0] != mouseEventfLeftDown || flags[1] != mouseEventfLeftUp {
		t.Fatalf("emitted flags = %#v, want exactly [leftDown, leftUp] — the button was not released", flags)
	}
	if framesAtEmit[0] != frameA {
		t.Fatalf("MouseDown ran under frame %+v, want the pre-invalidate frame %+v", framesAtEmit[0], frameA)
	}
	if framesAtEmit[1] != frameB {
		t.Fatalf("MouseUp ran under frame %+v, want the updated frame %+v — the update did not reach the live backend",
			framesAtEmit[1], frameB)
	}
}

// 6. TestSetWindow_ConcurrentWithInput: twenty goroutines re-measure and
// transform at once. Nothing may panic, the race detector must stay quiet, and
// — the part -race alone would NOT catch — no transform may be computed from a
// half-written Window (offset from one frame, scale from the other).
func TestSetWindow_ConcurrentWithInput(t *testing.T) {
	frameA := Window{ContentOffsetX: 100, ContentOffsetY: 200, Scale: 1.0}
	frameB := Window{ContentOffsetX: 1000, ContentOffsetY: 500, Scale: 1.25}

	b := New(TypeNative, Config{Window: frameA})
	n := b.(*nativeBackend)

	// The only two legal answers for css(100,80).
	wantA := [2]int32{200, 280}
	wantB := [2]int32{1125, 600}

	const goroutines, iterations = 20, 200
	var wg sync.WaitGroup
	bad := make(chan [2]int32, goroutines*iterations)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if i%2 == 0 {
					if j%2 == 0 {
						b.SetWindow(frameA)
					} else {
						b.SetWindow(frameB)
					}
					continue
				}
				got := [2]int32{}
				got[0], got[1] = n.toScreen(100, 80)
				if got != wantA && got != wantB {
					bad <- got
				}
				n.OSPositionCSS()
			}
		}(i)
	}
	wg.Wait()
	close(bad)

	if got, ok := <-bad; ok {
		t.Fatalf("toScreen produced (%d,%d) — neither frame's answer, so it read a half-written Window",
			got[0], got[1])
	}
}

// ---------------------------------------------------------------- scale ----

// 8. TestWindowScale_FromDPI: the kit derives the scale from the live window's
// DPI instead of trusting a number the caller decided before launch. 0 and an
// unavailable API both fall back to 1.0 — never to a degenerate zero-scale
// transform, which would collapse every CSS coordinate onto the content origin.
func TestWindowScale_FromDPI(t *testing.T) {
	orig := dpiForWindow
	t.Cleanup(func() { dpiForWindow = orig })

	cases := []struct {
		name string
		dpi  uint32
		ok   bool
		want float64
	}{
		{"96 dpi → 1.0", 96, true, 1.0},
		{"120 dpi → 1.25", 120, true, 1.25},
		{"144 dpi → 1.5", 144, true, 1.5},
		{"0 dpi → fallback 1.0", 0, true, 1.0},
		{"GetDpiForWindow unavailable → fallback 1.0", 120, false, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dpiForWindow = func(uintptr) (uint32, bool) { return tc.dpi, tc.ok }
			if got := MeasureDPIScale(0x1234); got != tc.want {
				t.Fatalf("MeasureDPIScale = %g, want %g", got, tc.want)
			}
		})
	}
}

//go:build windows

package chromekit

import (
	"context"
	"sync"
	"testing"

	"github.com/multiprofilevn/chromekit/input"
)

// A Page caches its backend, its Cursor and its Keyboard behind ONE sync.Once
// here, where firefoxkit has three. The lower risk is why this file is shorter
// than its firefoxkit twin — not a reason to skip it: the two kits have to stay
// interchangeable from the fleet's side, and a re-measure that silently stops
// reaching chrome's cursor would be found in production rather than here.
//
// Assertions are at the behaviour layer only — the screen pixel the cursor
// actually converted to, and the frame the keyboard actually emitted under.
// Reading p.backend directly would pass even if Cursor and Keyboard were left
// holding a stale object.

// frameBackend is a fake Backend that models the one thing the real native
// backend does with a Window: convert CSS coordinates into absolute screen
// pixels, and record where a click would physically have landed.
type frameBackend struct {
	mu     sync.Mutex
	win    input.Window
	moves  [][2]float64
	keyWin []input.Window
}

func (f *frameBackend) SetWindow(w input.Window) {
	f.mu.Lock()
	f.win = w
	f.mu.Unlock()
}

// screen mirrors the native backend's cssToScreen for the absolute-offset
// frame: ContentOffset carries the screen position of CSS (0,0), X/Y are zero.
func (f *frameBackend) screen(x, y float64) [2]float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.win.Scale
	if s <= 0 {
		s = 1
	}
	return [2]float64{
		float64(f.win.X+f.win.ContentOffsetX) + x*s,
		float64(f.win.Y+f.win.ContentOffsetY) + y*s,
	}
}

func (f *frameBackend) MouseMove(_ context.Context, x, y float64) error {
	p := f.screen(x, y)
	f.mu.Lock()
	f.moves = append(f.moves, p)
	f.mu.Unlock()
	return nil
}

func (f *frameBackend) MouseDown(_ context.Context, _, _ float64, _ input.MouseButton) error {
	return nil
}

func (f *frameBackend) MouseUp(_ context.Context, _, _ float64, _ input.MouseButton) error {
	return nil
}

func (f *frameBackend) MouseScroll(_ context.Context, _, _ float64) error { return nil }

func (f *frameBackend) KeyChar(_ context.Context, _ rune) error {
	f.mu.Lock()
	f.keyWin = append(f.keyWin, f.win)
	f.mu.Unlock()
	return nil
}

func (f *frameBackend) InsertText(_ context.Context, _ string) error { return nil }

func (f *frameBackend) KeyCombo(_ context.Context, _ uint16, _ input.Modifier) error {
	f.mu.Lock()
	f.keyWin = append(f.keyWin, f.win)
	f.mu.Unlock()
	return nil
}

func (f *frameBackend) lastMove() ([2]float64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.moves) == 0 {
		return [2]float64{}, false
	}
	return f.moves[len(f.moves)-1], true
}

func (f *frameBackend) lastKeyWindow() (input.Window, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.keyWin) == 0 {
		return input.Window{}, false
	}
	return f.keyWin[len(f.keyWin)-1], true
}

// 7 (chrome half). TestInvalidate_ChromeCursorAndKeyboardSeeNewWindow.
//
// Sequence matters: the cursor and the keyboard are PRIMED before the window
// moves. That is the real-world order — a job clicks, something repositions the
// window, the job clicks again — and it is the only order in which a stale
// capture is reachable.
func TestInvalidate_ChromeCursorAndKeyboardSeeNewWindow(t *testing.T) {
	prevMetrics, prevRect := pageWindowMetrics, browserWindowRect
	t.Cleanup(func() { pageWindowMetrics, browserWindowRect = prevMetrics, prevRect })

	// Window at (100,300), 800x600 outer, 784x520 inner → chrome 8px each side
	// horizontally, 80px on top → content origin (108, 380).
	pageWindowMetrics = func(*Browser) (windowMetrics, error) {
		return windowMetrics{ScreenX: 100, ScreenY: 300, OuterWidth: 800, OuterHeight: 600,
			InnerWidth: 784, InnerHeight: 520}, nil
	}
	browserWindowRect = func(*Browser) (input.Rect, error) {
		return input.Rect{Left: 100, Top: 300, Right: 900, Bottom: 900}, nil
	}

	cfg := defaultConfig()
	cfg.inputBackend = BackendNative
	cfg.cursorProfile = CursorMachine // no bezier cadence; this spec is about coordinates

	b := &Browser{log: noopLogger{}, cfg: cfg, ctx: context.Background()}
	fake := &frameBackend{}
	// attached: the stub stands in for a live page, so alive() must not try to
	// drive chromedp's lazy attach against this bare context.
	p := &Page{ctx: context.Background(), log: noopLogger{}, cfg: cfg, browser: b, attached: true}
	p.backend = fake // honoured by initInput()'s "pre-set by tests" path
	p.mouseAPI = &MouseAPI{p: p}
	p.keyboardAPI = &KeyboardAPI{p: p}
	b.pages = []*Page{p}
	b.current = p

	startFrame, err := chromeFrameFor(b)
	if err != nil {
		t.Fatalf("initial frame: %v", err)
	}
	fake.SetWindow(startFrame)

	// PRIME both lazy layers while the old frame is live.
	if err := p.Mouse().MoveTo(10, 10); err != nil {
		t.Fatalf("priming MoveTo: %v", err)
	}
	if err := p.Keyboard().Type("a"); err != nil {
		t.Fatalf("priming Type: %v", err)
	}

	// The window moves 800px right and 200px down.
	pageWindowMetrics = func(*Browser) (windowMetrics, error) {
		return windowMetrics{ScreenX: 900, ScreenY: 500, OuterWidth: 800, OuterHeight: 600,
			InnerWidth: 784, InnerHeight: 520}, nil
	}
	browserWindowRect = func(*Browser) (input.Rect, error) {
		return input.Rect{Left: 900, Top: 500, Right: 1700, Bottom: 1100}, nil
	}

	if err := b.InvalidateNativeWindow(); err != nil {
		t.Fatalf("InvalidateNativeWindow: %v", err)
	}
	wantFrame, err := chromeFrameFor(b)
	if err != nil {
		t.Fatalf("re-measured frame: %v", err)
	}

	// --- cursor: assert the SCREEN pixel a click would physically land on ---
	if err := p.Mouse().MoveTo(100, 80); err != nil {
		t.Fatalf("post-invalidate MoveTo: %v", err)
	}
	got, ok := fake.lastMove()
	if !ok {
		t.Fatal("no cursor movement was emitted at all")
	}
	want := [2]float64{
		float64(wantFrame.ContentOffsetX) + 100*wantFrame.Scale,
		float64(wantFrame.ContentOffsetY) + 80*wantFrame.Scale,
	}
	if got != want {
		t.Fatalf("cursor emitted screen (%g,%g), want (%g,%g) — the Cursor is still converting through the pre-move frame",
			got[0], got[1], want[0], want[1])
	}

	// --- keyboard: assert the frame it actually emitted under ---
	if err := p.Keyboard().Type("b"); err != nil {
		t.Fatalf("post-invalidate Type: %v", err)
	}
	gotWin, ok := fake.lastKeyWindow()
	if !ok {
		t.Fatal("no keystroke was emitted at all")
	}
	if gotWin != wantFrame {
		t.Fatalf("keyboard emitted under frame %+v, want %+v — the Keyboard is still holding the pre-move frame",
			gotWin, wantFrame)
	}
}

// chromeFrameFor computes the coordinate frame the page SHOULD be using right
// now, from the same measurements the production path reads. Deriving the
// expectation keeps this spec testing "the frame was refreshed" rather than
// "the frame equals these six numbers".
func chromeFrameFor(b *Browser) (input.Window, error) {
	ox, oy, err := b.ContentOffset()
	if err != nil {
		return input.Window{}, err
	}
	scale, serr := b.WindowScale()
	if serr != nil || scale <= 0 {
		scale = 1.0
	}
	return buildNativeWindow(b.cfg.nativeWindow, ox, oy, scale, true), nil
}

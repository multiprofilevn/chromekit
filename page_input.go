package chromekit

import (
	"fmt"
	"math/rand"

	"github.com/tuwibu/chromekit/input"
)

// initInput lazily builds the backend + helpers using the browser config.
// Called from every page input method via sync.Once.
func (p *Page) initInput() {
	p.inputOnce.Do(func() {
		t := input.TypeCDP
		if p.cfg != nil && p.cfg.inputBackend == BackendNative {
			t = input.TypeNative
		}
		if p.backend == nil { // may be pre-set by tests (fake backend injection)
			// The guard only means anything for the native backend — CDP
			// addresses the tab directly and never needs the foreground.
			var guard func() error
			if t == input.TypeNative {
				guard = p.browser.foregroundGuard()
			}
			p.backend = input.New(t, input.Config{Window: p.nativeWindow(t), ForegroundGuard: guard})
		}
		profile := input.ProfileHuman
		if p.cfg != nil {
			switch p.cfg.cursorProfile {
			case CursorFast:
				profile = input.ProfileFast
			case CursorMachine:
				profile = input.ProfileMachine
			}
		}
		mousePathURL := ""
		if p.cfg != nil {
			mousePathURL = p.cfg.mousePathURL
		}
		p.cursor = input.NewCursorWithProfileAndPath(p.backend, profile, mousePathURL)
		p.keyboard = input.NewKeyboard(p.backend)
		p.touch = input.NewTouchScreen(p.backend)
		p.inputReady.Store(true)
	})
}

// nativeWindow resolves the coordinate frame for the backend. It measures the
// live content origin + DPI scale from the Chrome HWND so css(0,0) maps onto the
// rendered content instead of the omnibox. Falls back to the manual
// WithNativeWindow values when the window cannot be measured (CDP backend, the
// Connect path before a window exists, non-Windows, or a content origin the
// window rectangle refused).
func (p *Page) nativeWindow(t input.Type) input.Window {
	var manual nativeWindow
	if p.cfg != nil {
		manual = p.cfg.nativeWindow
	}
	if t != input.TypeNative || p.browser == nil {
		return buildNativeWindow(manual, 0, 0, 0, false)
	}
	ox, oy, err := p.browser.ContentOffset()
	if err != nil {
		if p.log != nil {
			p.log.Warnf("chromekit: native frame not measurable (%v) — falling back to WithNativeWindow(%d,%d,%g)",
				err, manual.X, manual.Y, manual.Scale)
		}
		return buildNativeWindow(manual, 0, 0, 0, false)
	}
	scale, serr := p.browser.WindowScale()
	if serr != nil || scale <= 0 {
		scale = manual.Scale
	}
	return buildNativeWindow(manual, ox, oy, scale, true)
}

// buildNativeWindow chooses the coordinate frame for the native backend.
// measured=true → content origin carried as an ABSOLUTE screen position in
// ContentOffsetX/Y with X=Y=0, so cssToScreen has no window-position term to
// double-count. measured=false → use the manual WithNativeWindow values.
//
// Mirrors firefoxkit's buildNativeWindow; the two frames must stay identical in
// meaning or the fleet's drift guard cannot compute against both.
func buildNativeWindow(manual nativeWindow, ox, oy int, scale float64, measured bool) input.Window {
	if measured {
		if scale <= 0 {
			scale = 1.0
		}
		return input.Window{X: 0, Y: 0, ContentOffsetX: ox, ContentOffsetY: oy, Scale: scale}
	}
	s := manual.Scale
	if s <= 0 {
		s = 1.0
	}
	return input.Window{X: manual.X, Y: manual.Y, Scale: s}
}

// setInputWindow pushes a re-measured frame into this page's live backend.
//
// No-op until the backend exists: forcing construction here would build one for
// a page that never takes native input, and would do it from whatever the window
// happens to be doing at invalidation time rather than at first use.
//
// The backend is mutated, never replaced — p.backend, p.cursor and p.keyboard
// are read bare by roughly thirty call sites, and swapping the object between a
// MouseDown and its MouseUp leaves the OS holding the button down.
func (p *Page) setInputWindow(w input.Window) {
	if !p.inputReady.Load() {
		return
	}
	p.backend.SetWindow(w)
}

// ClickElement queries the selector, picks a random point inside its
// bounding box (with a small inset so we don't graze the edge) and clicks.
// Selector-based high-level shortcut — kept on Page deliberately.
func (p *Page) ClickElement(selector string) error {
	if err := p.alive(); err != nil {
		return err
	}
	node, err := p.QuerySelector(selector, 0)
	if err != nil {
		return err
	}
	box, err := p.BoundingBox(node)
	if err != nil {
		return err
	}
	if box.Width <= 0 || box.Height <= 0 {
		return fmt.Errorf("%w: zero-size element %q", ErrEvaluate, selector)
	}
	inset := 4.0
	if box.Width < inset*2 || box.Height < inset*2 {
		inset = 1.0
	}
	x := box.Left + inset + rand.Float64()*(box.Width-inset*2)
	y := box.Top + inset + rand.Float64()*(box.Height-inset*2)
	p.initInput()
	ctx, cancel := opCtx(p.ctx, 0)
	defer cancel()
	return p.cursor.Click(ctx, x, y, input.MouseLeft)
}

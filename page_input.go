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
		var win input.Window
		if p.cfg != nil {
			win = input.Window{
				X:     p.cfg.nativeWindow.X,
				Y:     p.cfg.nativeWindow.Y,
				Scale: p.cfg.nativeWindow.Scale,
			}
		}
		// Auto-detect chrome chrome offset (title bar + tabs + omnibox) so
		// CSS (0,0) — page content origin — maps to the right screen pixel
		// instead of landing on the address bar. Best-effort: silent fallback
		// to (0,0) outside Windows or before a window handle is reachable.
		if p.browser != nil && t == input.TypeNative {
			if ox, oy, err := p.browser.ContentOffset(); err == nil {
				win.ContentOffsetX = ox
				win.ContentOffsetY = oy
			}
		}
		p.backend = input.New(t, input.Config{Window: win})
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
	})
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
	return p.cursor.Click(p.ctx, x, y, input.MouseLeft)
}

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
		p.cursor = input.NewCursorWithProfile(p.backend, profile)
		p.keyboard = input.NewKeyboard(p.backend)
		p.touch = input.NewTouchScreen(p.backend)
	})
}

// MoveTo glides the cursor to (x, y) in viewport CSS pixels.
func (p *Page) MoveTo(x, y float64) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.initInput()
	return p.cursor.MoveTo(p.ctx, x, y)
}

// ClickAt clicks at (x, y) with the left button.
func (p *Page) ClickAt(x, y float64) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.initInput()
	return p.cursor.Click(p.ctx, x, y, input.MouseLeft)
}

// ClickElement queries the selector, picks a random point inside its
// bounding box (with a small inset so we don't graze the edge) and clicks.
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
	return p.ClickAt(x, y)
}

// TypeHuman types each rune with random delays. Best when a real user input
// is being mimicked (e.g. forms watched by anti-bot scripts).
func (p *Page) TypeHuman(text string) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.initInput()
	return p.keyboard.Type(p.ctx, text)
}

// TypeFast pastes the text in one event (CDP) or per-rune without delay (native).
func (p *Page) TypeFast(text string) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.initInput()
	return p.keyboard.TypeFast(p.ctx, text)
}

// Tap dispatches a touch tap at (x, y).
func (p *Page) Tap(x, y float64) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.initInput()
	return p.touch.Tap(p.ctx, x, y)
}

// Scroll dispatches a wheel event with the given deltas.
func (p *Page) Scroll(deltaX, deltaY float64) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.initInput()
	return p.cursor.Scroll(p.ctx, deltaX, deltaY)
}

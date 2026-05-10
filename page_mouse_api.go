package chromekit

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/tuwibu/chromekit/input"
)

const dragSteps = 10

// MOUSE_PADDING_WIDTH / MOUSE_PADDING_HEIGHT define the central fraction of
// an element's bounding box where elementClickPoint may pick a click point.
// 0.3 wide × 0.1 tall → clicks land in a flat horizontal strip near the
// vertical center: human-ish jitter without drifting toward edges where
// hit-testing can flip to a sibling. Tweak per project's tolerance.
const (
	MOUSE_PADDING_WIDTH  = 0.3
	MOUSE_PADDING_HEIGHT = 0.1
)

// MouseAPI groups every mouse primitive — both raw-coordinate (low-level)
// and selector-based extras. The high-level shortcuts p.Click(sel) and
// p.ClickElement(sel) stay on Page so common automation reads naturally.
type MouseAPI struct{ p *Page }

// MoveTo glides the cursor to (x, y) in viewport CSS pixels.
func (m *MouseAPI) MoveTo(x, y float64) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	m.p.initInput()
	return m.p.cursor.MoveTo(m.p.ctx, x, y)
}

// ClickAt clicks at (x, y) with the left button.
func (m *MouseAPI) ClickAt(x, y float64) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	m.p.initInput()
	return m.p.cursor.Click(m.p.ctx, x, y, input.MouseLeft)
}

// Scroll dispatches a wheel event with the given deltas.
func (m *MouseAPI) Scroll(deltaX, deltaY float64) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	m.p.initInput()
	return m.p.cursor.Scroll(m.p.ctx, deltaX, deltaY)
}

// Tap dispatches a touch tap at (x, y).
func (m *MouseAPI) Tap(x, y float64) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	m.p.initInput()
	return m.p.touch.Tap(m.p.ctx, x, y)
}

// Click moves the cursor to the center of the element matching selector with
// a human-like path, then performs a left click. Different from p.Click which
// dispatches an instant CDP click without cursor movement.
func (m *MouseAPI) Click(selector string) error {
	x, y, err := m.p.elementClickPoint(selector)
	if err != nil {
		return err
	}
	m.p.initInput()
	return m.p.cursor.Click(m.p.ctx, x, y, input.MouseLeft)
}

// FocusElement scrolls the element into view, performs a human-like click on
// it, and verifies document.activeElement landed on the target. Falls back to
// chromedp.Focus (DOM .focus()) if the click didn't take focus — handles
// disabled-but-focusable widgets, custom controls, and overlay-occluded
// inputs. Returns an error only when both paths fail.
func (m *MouseAPI) FocusElement(selector string) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	// Best-effort scroll — non-scrollable layouts return an error we can ignore.
	_ = m.p.ScrollIntoView(selector)
	if err := m.Click(selector); err != nil {
		return err
	}
	var focused bool
	expr := fmt.Sprintf(
		"!!document.activeElement && document.activeElement.matches(%s)",
		strconv.Quote(selector),
	)
	if err := m.p.Evaluate(expr, &focused); err == nil && focused {
		return nil
	}
	if err := chromedp.Run(m.p.ctx, chromedp.Focus(selector, chromedp.ByQuery)); err != nil {
		return fmt.Errorf("%w: focus %q: %s", ErrEvaluate, selector, err.Error())
	}
	return nil
}

// Hover moves the cursor to the center of the element matching selector. No
// click. Useful to trigger :hover styles or hover-only menus.
func (m *MouseAPI) Hover(selector string) error {
	x, y, err := m.p.elementClickPoint(selector)
	if err != nil {
		return err
	}
	m.p.initInput()
	return m.p.cursor.MoveTo(m.p.ctx, x, y)
}

// DoubleClick fires two left clicks at the element's center within ~120ms —
// the standard double-click window in browsers.
func (m *MouseAPI) DoubleClick(selector string) error {
	x, y, err := m.p.elementClickPoint(selector)
	if err != nil {
		return err
	}
	m.p.initInput()
	if err := m.p.cursor.Click(m.p.ctx, x, y, input.MouseLeft); err != nil {
		return err
	}
	time.Sleep(60 * time.Millisecond)
	return m.p.cursor.Click(m.p.ctx, x, y, input.MouseLeft)
}

// RightClick clicks the element's center with the right mouse button. Most
// pages open a context menu — chromekit does NOT auto-dismiss it; the caller
// either picks an item or calls Keyboard().Press("Escape").
func (m *MouseAPI) RightClick(selector string) error {
	x, y, err := m.p.elementClickPoint(selector)
	if err != nil {
		return err
	}
	m.p.initInput()
	return m.p.cursor.Click(m.p.ctx, x, y, input.MouseRight)
}

// DragAndDrop holds the left button down at fromSel's center, glides through
// ~10 intermediate points, and releases at toSel's center. The intermediate
// moves are needed because HTML5 drag fires only after the pointer crosses a
// distance threshold — a single jump won't trigger it.
func (m *MouseAPI) DragAndDrop(fromSel, toSel string) error {
	fx, fy, err := m.p.elementClickPoint(fromSel)
	if err != nil {
		return err
	}
	tx, ty, err := m.p.elementClickPoint(toSel)
	if err != nil {
		return err
	}
	return m.Drag(fx, fy, tx, ty)
}

// Drag is the coordinate variant of DragAndDrop. Use when you have raw
// points (e.g. resizing a slider thumb to a measured pixel offset).
func (m *MouseAPI) Drag(fromX, fromY, toX, toY float64) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	m.p.initInput()
	if err := m.p.cursor.MoveTo(m.p.ctx, fromX, fromY); err != nil {
		return err
	}
	if err := m.p.backend.MouseDown(m.p.ctx, fromX, fromY, input.MouseLeft); err != nil {
		return err
	}
	dx := (toX - fromX) / float64(dragSteps)
	dy := (toY - fromY) / float64(dragSteps)
	for i := 1; i <= dragSteps; i++ {
		x := fromX + dx*float64(i)
		y := fromY + dy*float64(i)
		if err := m.p.backend.MouseMove(m.p.ctx, x, y); err != nil {
			_ = m.p.backend.MouseUp(m.p.ctx, x, y, input.MouseLeft)
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return m.p.backend.MouseUp(m.p.ctx, toX, toY, input.MouseLeft)
}

// elementClickPoint resolves selector to a click point inside its bounding
// box. Picks a random point within the central MOUSE_PADDING_WIDTH ×
// MOUSE_PADDING_HEIGHT fraction of the box instead of the exact midpoint —
// repeated clicks on the same element land at slightly different pixels,
// which reads as human and avoids fingerprint patterns that flag dead-center
// clicks. Returns ErrEvaluate when the box has zero size (element collapsed
// / display:none).
func (p *Page) elementClickPoint(selector string) (float64, float64, error) {
	if err := p.alive(); err != nil {
		return 0, 0, err
	}
	node, err := p.QuerySelector(selector, 0)
	if err != nil {
		return 0, 0, err
	}
	box, err := p.BoundingBox(node)
	if err != nil {
		return 0, 0, err
	}
	if box.Width <= 0 || box.Height <= 0 {
		return 0, 0, fmt.Errorf("%w: zero-size element %q", ErrEvaluate, selector)
	}
	cx := box.Left + box.Width/2
	cy := box.Top + box.Height/2
	halfW := box.Width * MOUSE_PADDING_WIDTH / 2
	halfH := box.Height * MOUSE_PADDING_HEIGHT / 2
	x := cx + (rand.Float64()*2-1)*halfW
	y := cy + (rand.Float64()*2-1)*halfH
	return x, y, nil
}

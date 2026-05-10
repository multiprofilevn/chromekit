package chromekit

import (
	"fmt"
	"time"

	"github.com/tuwibu/chromekit/input"
)

const dragSteps = 10

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

// Hover moves the cursor to the center of the element matching selector. No
// click. Useful to trigger :hover styles or hover-only menus.
func (m *MouseAPI) Hover(selector string) error {
	x, y, err := m.p.elementCenter(selector)
	if err != nil {
		return err
	}
	m.p.initInput()
	return m.p.cursor.MoveTo(m.p.ctx, x, y)
}

// DoubleClick fires two left clicks at the element's center within ~120ms —
// the standard double-click window in browsers.
func (m *MouseAPI) DoubleClick(selector string) error {
	x, y, err := m.p.elementCenter(selector)
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
	x, y, err := m.p.elementCenter(selector)
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
	fx, fy, err := m.p.elementCenter(fromSel)
	if err != nil {
		return err
	}
	tx, ty, err := m.p.elementCenter(toSel)
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

// elementCenter resolves selector to its bounding box center in CSS px.
// Centralizes the QuerySelector → BoundingBox → midpoint dance so mouse
// extras and drag share one path. Lives on Page (private) so frame-side
// helpers can reuse it later.
func (p *Page) elementCenter(selector string) (float64, float64, error) {
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
	return box.Left + box.Width/2, box.Top + box.Height/2, nil
}

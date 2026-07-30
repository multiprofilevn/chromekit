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

// MoveTo glides the cursor to (x, y) in viewport CSS pixels. Bounded by
// DefaultActionTimeout (including any time spent queued behind the gesture
// limiter), so a wedged renderer fails the move instead of hanging forever.
func (m *MouseAPI) MoveTo(x, y float64) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	m.p.initInput()
	ctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	return m.p.cursor.MoveTo(ctx, x, y)
}

// ClickAt clicks at (x, y) with the left button.
func (m *MouseAPI) ClickAt(x, y float64) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	m.p.initInput()
	ctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	return m.p.cursor.Click(ctx, x, y, input.MouseLeft)
}

// Scroll dispatches a wheel event with the given deltas.
func (m *MouseAPI) Scroll(deltaX, deltaY float64) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	m.p.initInput()
	ctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	return m.p.cursor.Scroll(ctx, deltaX, deltaY)
}

// Tap dispatches a touch tap at (x, y).
func (m *MouseAPI) Tap(x, y float64) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	m.p.initInput()
	ctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	return m.p.touch.Tap(ctx, x, y)
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
	ctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	return m.p.cursor.Click(ctx, x, y, input.MouseLeft)
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
	fctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	if err := chromedp.Run(fctx, chromedp.Focus(selector, chromedp.ByQuery)); err != nil {
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
	ctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	return m.p.cursor.MoveTo(ctx, x, y)
}

// DoubleClick fires two left clicks at the element's center within ~120ms —
// the standard double-click window in browsers.
//
// The gesture holds one process-wide limiter slot across both clicks, but NOT
// Cursor.mu for the whole window — c.mu is taken and released per Click. The
// clicks still land on the fixed x,y (passed by value), but a caller driving
// the same page's Cursor from another goroutine mid-DoubleClick can move the
// pointer between the two clicks. As elsewhere, one Cursor is meant to be
// driven from a single goroutine.
func (m *MouseAPI) DoubleClick(selector string) error {
	x, y, err := m.p.elementClickPoint(selector)
	if err != nil {
		return err
	}
	m.p.initInput()
	opctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	// Hold ONE gesture slot across both clicks. Otherwise the second Click
	// queues independently behind the process-wide limiter, and under load its
	// wait can exceed the double-click window — the browser then sees two
	// separate single clicks instead of a double click. The deadline bounds the
	// slot wait too (opCtx wraps the acquire).
	ctx, release, err := input.AcquireGesture(opctx)
	if err != nil {
		return err
	}
	defer release()
	if err := m.p.cursor.Click(ctx, x, y, input.MouseLeft); err != nil {
		return err
	}
	time.Sleep(60 * time.Millisecond)
	return m.p.cursor.Click(ctx, x, y, input.MouseLeft)
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
	ctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	return m.p.cursor.Click(ctx, x, y, input.MouseRight)
}

// MiddleClick clicks at (x, y) with the middle button — firefoxkit parity
// (chromekit's MouseAPI previously had no middle-button primitive).
func (m *MouseAPI) MiddleClick(x, y float64) error {
	if err := m.p.alive(); err != nil {
		return err
	}
	m.p.initInput()
	ctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	return m.p.cursor.Click(ctx, x, y, input.MouseMiddle)
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
	opctx, cancel := opCtx(m.p.ctx, 0)
	defer cancel()
	// One gesture slot for the whole press → intermediate moves → release, so
	// the drag is never split apart by the process-wide limiter mid-flight.
	ctx, release, err := input.AcquireGesture(opctx)
	if err != nil {
		return err
	}
	defer release()
	if err := m.p.cursor.MoveTo(ctx, fromX, fromY); err != nil {
		return err
	}
	if err := m.p.backend.MouseDown(ctx, fromX, fromY, input.MouseLeft); err != nil {
		return err
	}
	dx := (toX - fromX) / float64(dragSteps)
	dy := (toY - fromY) / float64(dragSteps)
	for i := 1; i <= dragSteps; i++ {
		x := fromX + dx*float64(i)
		y := fromY + dy*float64(i)
		if err := m.p.backend.MouseMove(ctx, x, y); err != nil {
			_ = m.p.backend.MouseUp(ctx, x, y, input.MouseLeft)
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return m.p.backend.MouseUp(ctx, toX, toY, input.MouseLeft)
}

// settleMinMs / settleMaxMs cover smooth-scroll animation durations (Twitter,
// Material, Tailwind sites all animate scroll 200-700ms). Lower bound 500ms
// to outlast common cubic-bezier curves; upper 1000ms to bound worst case.
const (
	settleMinMs = 500
	settleMaxMs = 1000
)

// elementClickPoint resolves selector to a click point inside the element's
// viewport rect. Sequence:
//
//  1. QuerySelector — confirm element exists.
//  2. ScrollIntoView({block:'center', inline:'center'}) — guarantee element
//     is in viewport. Necessary for native (OS-level SendInput) backends:
//     clicks at off-viewport CSS coords land on whatever's currently rendered
//     at that pixel (often nothing / sidebar / wrong element).
//  3. Sleep 500-1000ms (randomised) — let smooth-scroll animation settle.
//     Measuring mid-animation returns in-flight coords; the actual click
//     fires later when the element has moved.
//  4. getBoundingClientRect via JS — viewport-relative, post-layout, fresh.
//     Preferred over dom.GetBoxModel (CDP layout-tree) which can return
//     content/border quad of an INNER child for compound DOM (button → span
//     → text) → coords land outside the button's hit area, click missed.
//  5. Pick random point in central MOUSE_PADDING_WIDTH × MOUSE_PADDING_HEIGHT
//     band around center → human-ish jitter, avoids edge hit-test flips to
//     siblings, avoids dead-center fingerprint pattern.
//
// Returns ErrEvaluate when zero-size or selector miss.
func (p *Page) elementClickPoint(selector string) (float64, float64, error) {
	if err := p.alive(); err != nil {
		return 0, 0, err
	}
	if _, err := p.QuerySelector(selector, 0); err != nil {
		return 0, 0, err
	}
	if err := p.ScrollIntoView(selector); err != nil {
		return 0, 0, err
	}
	time.Sleep(time.Duration(settleMinMs+rand.Intn(settleMaxMs-settleMinMs+1)) * time.Millisecond)

	rectJS := fmt.Sprintf(`(() => {
		const el = document.querySelector(%s);
		if (!el) return null;
		const r = el.getBoundingClientRect();
		return { x: r.x, y: r.y, w: r.width, h: r.height };
	})()`, strconv.Quote(selector))
	var rect struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	}
	if err := p.Evaluate(rectJS, &rect); err != nil {
		return 0, 0, fmt.Errorf("%w: getBoundingClientRect %q: %s", ErrEvaluate, selector, err.Error())
	}
	if rect.W <= 0 || rect.H <= 0 {
		return 0, 0, fmt.Errorf("%w: zero-size element %q (rect=%vx%v)", ErrEvaluate, selector, rect.W, rect.H)
	}
	cx := rect.X + rect.W/2
	cy := rect.Y + rect.H/2
	halfW := rect.W * MOUSE_PADDING_WIDTH / 2
	halfH := rect.H * MOUSE_PADDING_HEIGHT / 2
	x := cx + (rand.Float64()*2-1)*halfW
	y := cy + (rand.Float64()*2-1)*halfH
	return x, y, nil
}

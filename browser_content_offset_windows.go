//go:build windows

package chromekit

import (
	"fmt"

	"github.com/tuwibu/chromekit/input"
)

// windowMetrics is the geometry Chrome reports about its own window, read from
// inside the page realm.
//
// UNTRUSTED. Every field here is a property of the page's `window` object and
// page script can redefine all of them. Treat the numbers as a hint that must
// be bounded by an out-of-band measurement before anything moves the physical
// cursor with them.
type windowMetrics struct {
	ScreenX     int `json:"screenX"`
	ScreenY     int `json:"screenY"`
	OuterWidth  int `json:"outerWidth"`
	OuterHeight int `json:"outerHeight"`
	InnerWidth  int `json:"innerWidth"`
	InnerHeight int `json:"innerHeight"`
}

// Seams over the two independent sources ContentOffset draws on. Tests replace
// them so the spoofing case can be exercised without a real browser and the
// trusted bound can be moved independently of the untrusted measurement.
var (
	pageWindowMetrics = readPageWindowMetrics
	browserWindowRect = readBrowserWindowRect
)

// readPageWindowMetrics asks the renderer for its window geometry.
func readPageWindowMetrics(b *Browser) (windowMetrics, error) {
	page := b.Current()
	if page == nil {
		return windowMetrics{}, fmt.Errorf("contentOffset: no current page")
	}
	var info windowMetrics
	if err := page.Evaluate(`({
		screenX: window.screenX, screenY: window.screenY,
		outerWidth: window.outerWidth, outerHeight: window.outerHeight,
		innerWidth: window.innerWidth, innerHeight: window.innerHeight,
	})`, &info); err != nil {
		return windowMetrics{}, fmt.Errorf("contentOffset: query window metrics: %w", err)
	}
	return info, nil
}

// readBrowserWindowRect reads the window rectangle from the window manager.
func readBrowserWindowRect(b *Browser) (input.Rect, error) {
	hwnd, err := b.HWND()
	if err != nil {
		return input.Rect{}, err
	}
	return input.MeasureWindowRect(hwnd)
}

// ContentOffset returns the ABSOLUTE screen position of the page's CSS (0,0) —
// the top-left pixel of the rendered content area. Same semantics as
// firefoxkit.Browser.ContentOffset: the value is a screen coordinate, not an
// offset from any window origin, so callers must never add a window position to
// it.
//
// Win32 GetClientRect / ClientToScreen do NOT work here because Chrome on
// Windows draws its own title bar / tabs INSIDE the Win32 client area (custom
// window chrome). The measurement therefore has to come from the renderer:
//
//	contentTop  = window.screenY + (outerHeight - innerHeight)
//	contentLeft = window.screenX + (outerWidth  - innerWidth ) / 2
//
// Those are properties of the page's own window object, which page script can
// redefine. Because the native backend drives the PHYSICAL cursor, an inflated
// value steers the real pointer out of the browser and fires a real click on
// whatever it lands on. The result is therefore bounded by GetWindowRect, which
// answers to the window manager and is out of the page's reach; an origin that
// cannot be reconciled with the window rectangle is refused outright.
//
// Cached after the first successful call; InvalidateNativeWindow clears it.
func (b *Browser) ContentOffset() (int, int, error) {
	b.contentMu.Lock()
	if b.contentCached {
		ox, oy := b.contentOffsetX, b.contentOffsetY
		b.contentMu.Unlock()
		return ox, oy, nil
	}
	b.contentMu.Unlock()

	info, err := pageWindowMetrics(b)
	if err != nil {
		return 0, 0, err
	}

	chromeY := info.OuterHeight - info.InnerHeight
	chromeX := (info.OuterWidth - info.InnerWidth) / 2 // borders split L/R
	ox := info.ScreenX + chromeX
	oy := info.ScreenY + chromeY

	if err := b.boundContentOrigin(ox, oy); err != nil {
		return 0, 0, err
	}

	b.contentMu.Lock()
	b.contentOffsetX, b.contentOffsetY = ox, oy
	b.contentCached = true
	b.contentMu.Unlock()
	return ox, oy, nil
}

// boundContentOrigin rejects a page-measured content origin that cannot be
// reconciled with the window manager's rectangle.
//
// Fails closed on purpose, including when the rectangle itself is unavailable:
// without the out-of-band bound there is nothing left but the page's own word
// for where the physical cursor should go. Callers that genuinely cannot resolve
// an HWND declare the frame up front with WithNativeWindow instead.
func (b *Browser) boundContentOrigin(ox, oy int) error {
	rect, err := browserWindowRect(b)
	if err != nil {
		b.log.Errorf("chromekit: content origin (%d,%d) cannot be bounded — no window rectangle: %v", ox, oy, err)
		return fmt.Errorf("contentOffset: no out-of-band window rectangle to bound the page-measured origin: %w", err)
	}
	tol := DefaultContentOffsetTolerance
	if b.cfg != nil && b.cfg.contentOffsetTolerance > 0 {
		tol = b.cfg.contentOffsetTolerance
	}
	if ox >= rect.Left-tol && ox <= rect.Right+tol && oy >= rect.Top-tol && oy <= rect.Bottom+tol {
		return nil
	}
	b.log.Errorf("chromekit: page-reported content origin (%d,%d) lies outside window rect %+v by more than %dpx — refusing to drive the cursor with it",
		ox, oy, rect, tol)
	return fmt.Errorf("contentOffset: content origin (%d,%d) outside window rect %+v (tolerance %dpx)", ox, oy, rect, tol)
}

// WindowScale returns the per-monitor DPI scale factor of the Chrome window
// (1.0 at 96 DPI). Measured from the live HWND rather than taken from the
// caller, so it stays correct after the window moves to a monitor with a
// different scaling factor.
func (b *Browser) WindowScale() (float64, error) {
	hwnd, err := b.HWND()
	if err != nil {
		return 0, err
	}
	return input.MeasureDPIScale(hwnd), nil
}

// WindowRect returns the window's outer rectangle in absolute screen pixels,
// straight from the window manager. Callers use it as the out-of-band bound on
// coordinates that were measured inside the page.
func (b *Browser) WindowRect() (input.Rect, error) {
	return browserWindowRect(b)
}

// InvalidateNativeWindow re-measures the coordinate frame (content origin + DPI
// scale) and pushes it into the live input backends via Backend.SetWindow.
//
// Call it after anything that can move or rescale the window. The backend
// objects are updated IN PLACE — no Page field is ever reassigned — so an
// operation already in flight cannot be torn in half by the update.
func (b *Browser) InvalidateNativeWindow() error {
	b.contentMu.Lock()
	b.contentCached = false
	b.contentMu.Unlock()

	ox, oy, err := b.ContentOffset()
	if err != nil {
		return err
	}
	scale, serr := b.WindowScale()
	if serr != nil || scale <= 0 {
		scale = 1.0
	}
	var manual nativeWindow
	if b.cfg != nil {
		manual = b.cfg.nativeWindow
	}
	w := buildNativeWindow(manual, ox, oy, scale, true)
	for _, p := range b.Pages() {
		p.setInputWindow(w)
	}
	return nil
}

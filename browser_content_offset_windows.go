//go:build windows

package chromekit

import (
	"fmt"
	"math"

	"github.com/multiprofilevn/chromekit/input"
)

// windowMetrics is the geometry Chrome reports about its own window, read from
// inside the page realm. Every length here is in CSS pixels; DevicePixelRatio
// is the factor that turns them into physical screen pixels.
//
// UNTRUSTED. Every field here is a property of the page's `window` object and
// page script can redefine all of them. Treat the numbers as a hint that must
// be bounded by an out-of-band measurement before anything moves the physical
// cursor with them.
type windowMetrics struct {
	ScreenX          int     `json:"screenX"`
	ScreenY          int     `json:"screenY"`
	OuterWidth       int     `json:"outerWidth"`
	OuterHeight      int     `json:"outerHeight"`
	InnerWidth       int     `json:"innerWidth"`
	InnerHeight      int     `json:"innerHeight"`
	DevicePixelRatio float64 `json:"devicePixelRatio"`
}

// Seams over the three independent sources the coordinate frame draws on. Tests
// replace them so the spoofing case can be exercised without a real browser and
// the trusted bounds can be moved independently of the untrusted measurement.
var (
	pageWindowMetrics = readPageWindowMetrics
	browserWindowRect = readBrowserWindowRect
	osDPIScale        = readOSDPIScale
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
		devicePixelRatio: window.devicePixelRatio,
	})`, &info); err != nil {
		return windowMetrics{}, fmt.Errorf("contentOffset: query window metrics: %w", err)
	}
	return info, nil
}

// readOSDPIScale returns the window manager's DPI ratio for this window, or 1.0
// when there is no HWND to ask. Trusted, but blind to a forced device scale
// factor — see resolveWindowScale.
func readOSDPIScale(b *Browser) float64 {
	hwnd, err := b.HWND()
	if err != nil {
		return 1.0
	}
	return input.MeasureDPIScale(hwnd)
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
// window chrome). The measurement therefore has to come from the renderer, in
// CSS pixels, and be scaled into physical ones:
//
//	border      = (outerWidth  - innerWidth) / 2
//	contentLeft = (window.screenX + border                             ) * scale
//	contentTop  = (window.screenY + (outerHeight - innerHeight) - border) * scale
//
// outerHeight - innerHeight is NOT the height of the chrome above the content:
// it also contains the resize border BELOW it, which sits under the content and
// must not be added to the origin. Windows gives Chrome an equal border left,
// right and bottom, so the horizontal term measures the vertical one. Both
// measured configurations agree: unscaled, (1920-1904)/2 = 8 and 1080-929 = 151,
// so the chrome above the content is 143; at forced scale 1/3, (1920-1896)/2 =
// 12 and 1080-981 = 99, so it is 87 CSS px = 29 physical.
//
// The correction is worth 8px unscaled — small enough that clicks still landed
// inside a link and it went unnoticed — and 12 CSS px at 1/3, which is more than
// half the height of a 21px result link. Measured with a click recorder in the
// page: aiming at a link's centre y=256.3 delivered the click at y=267, one
// pixel past the bottom edge of its box, onto the enclosing <h2>.
//
// The scale term is not decoration. Under --force-device-scale-factor Chrome
// reports these lengths in a CSS pixel that is no longer a screen pixel:
// measured at 0.3333 with a 1920x1080 logical window, screenX came back 2358
// while GetWindowRect put the window's left edge at 786 — the same edge, in the
// two different units. Multiplying by the resolved scale is what reconciles
// them (2358 x 0.3333 = 786).
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
	rect, rerr := browserWindowRect(b)
	if rerr != nil {
		b.log.Errorf("chromekit: content origin cannot be bounded — no window rectangle: %v", rerr)
		return 0, 0, fmt.Errorf("contentOffset: no out-of-band window rectangle to bound the page-measured origin: %w", rerr)
	}

	scale := resolveWindowScale(info, rect, osDPIScale(b), b.warnf)
	chromeX := float64(info.OuterWidth-info.InnerWidth) / 2         // borders split L/R
	chromeY := float64(info.OuterHeight-info.InnerHeight) - chromeX // bottom border is NOT above the content
	if chromeY < 0 {
		chromeY = 0
	}
	ox := int(math.Round((float64(info.ScreenX) + chromeX) * scale))
	oy := int(math.Round((float64(info.ScreenY) + chromeY) * scale))

	if err := b.boundContentOrigin(ox, oy, rect); err != nil {
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
// The rectangle is read by the caller and passed in, so the origin is bounded
// by the SAME rectangle that resolved the scale — re-reading it here would let
// a window move between the two and turn a legitimate origin into a refusal.
// The caller fails closed when no rectangle is available at all: without the
// out-of-band bound there is nothing left but the page's own word for where the
// physical cursor should go. Callers that genuinely cannot resolve an HWND
// declare the frame up front with WithNativeWindow instead.
func (b *Browser) boundContentOrigin(ox, oy int, rect input.Rect) error {
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

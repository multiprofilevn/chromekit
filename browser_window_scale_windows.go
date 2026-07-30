//go:build windows

package chromekit

import (
	"math"

	"github.com/tuwibu/chromekit/input"
)

// windowScaleTolerance is the fraction by which the page's own
// devicePixelRatio may differ from the independent GetWindowRect estimate of
// the same ratio before the page value is refused.
//
// 2%, and the number is deliberately far above what was measured. Four real
// windows (Chrome and Firefox, each at forced scale 0.3333 and at 1.0) all
// agreed to the last digit the print carried — |dpr - rect/outerWidth| =
// 0.000000, 0.0000% relative, on every one. The only error a healthy window can
// produce is the browser rounding outerWidth to a whole CSS pixel, which caps
// the deviation at dpr*0.5/outerWidth: 0.00016 for the 1041px-wide scaled
// window, 0.00026 for the 1920px unscaled one. 2% is ~75x that bound and still
// two orders of magnitude below the failure this guards against, where the two
// numbers differ by a whole factor (1.0 vs 0.3333 — 200% apart).
const windowScaleTolerance = 0.02

// Plausible bounds on a device scale factor. Chrome's
// --force-device-scale-factor and Firefox's layout.css.devPixelsPerPx both live
// well inside this range, as does every shipping OS DPI setting; a page
// reporting outside it is not describing a real window.
const (
	minPlausibleScale = 0.1
	maxPlausibleScale = 10.0
)

// resolveWindowScale returns the CSS->physical-pixel ratio to map coordinates
// with, given the page's own claim and an out-of-band window rectangle.
//
// window.devicePixelRatio is BY DEFINITION that ratio, and is the only source
// that stays right in both cases that matter: with a forced device scale factor
// the browser ignores OS DPI entirely, and without one it follows it. The OS
// DPI ratio alone is right only in the second case — which is exactly the bug
// this function exists to close, where a CSS x of ~917 got multiplied by 1.0
// instead of 0.3333 and the cursor was aimed at x=2197 on a 1920-wide screen.
//
// It is also read from the page realm, where script can redefine it, so it is
// never trusted on its own. physicalWindowWidth / cssOuterWidth estimates the
// same ratio from a number the window manager owns; the page value is accepted
// only when the two independent estimates agree, and the trusted OS DPI ratio
// is used otherwise. The fallback is logged with both numbers, because a silent
// downgrade here turns into a permanent, untraceable click offset.
func resolveWindowScale(info windowMetrics, rect input.Rect, osScale float64, warnf func(string, ...any)) float64 {
	if osScale <= 0 {
		osScale = 1.0
	}
	physW := float64(rect.Right - rect.Left)
	physH := float64(rect.Bottom - rect.Top)
	dpr := info.DevicePixelRatio

	if math.IsNaN(dpr) || dpr < minPlausibleScale || dpr > maxPlausibleScale ||
		info.OuterWidth < 1 || info.OuterHeight < 1 || physW < 1 || physH < 1 {
		warnf("chromekit: page-reported devicePixelRatio %v with outer size %dx%d and window rect %.0fx%.0f is not a usable measurement — using the OS DPI ratio %g",
			dpr, info.OuterWidth, info.OuterHeight, physW, physH, osScale)
		return osScale
	}

	estW := physW / float64(info.OuterWidth)
	estH := physH / float64(info.OuterHeight)
	if scaleAgrees(dpr, estW) && scaleAgrees(dpr, estH) {
		return dpr
	}
	warnf("chromekit: page-reported devicePixelRatio %g disagrees with the window-rect estimate (%g across, %g down, tolerance %g%%) — using the OS DPI ratio %g instead",
		dpr, estW, estH, windowScaleTolerance*100, osScale)
	return osScale
}

// scaleAgrees reports whether two independent estimates of the same ratio are
// within windowScaleTolerance of each other, relative to the trusted one.
func scaleAgrees(dpr, est float64) bool {
	if est <= 0 {
		return false
	}
	return math.Abs(dpr-est) <= windowScaleTolerance*est
}

// WindowScale returns the ratio between the page's CSS pixels and physical
// screen pixels — the number the native backend multiplies CSS coordinates by.
//
// This is NOT simply the OS DPI ratio. When a device scale factor is forced
// (--force-device-scale-factor) the browser ignores OS DPI and renders at the
// forced value instead, so GetDpiForWindow answers a question nobody asked. The
// page's own devicePixelRatio covers both cases, and is cross-checked against
// GetWindowRect before it is believed — see resolveWindowScale.
//
// Measured from the live window rather than taken from the caller, so it stays
// correct after the window moves to a monitor with different scaling. Falls
// back to the OS DPI ratio, without complaint, whenever there is no page to ask
// yet — the launch path, before the first navigation.
func (b *Browser) WindowScale() (float64, error) {
	hwnd, err := b.HWND()
	if err != nil {
		return 0, err
	}
	osScale := input.MeasureDPIScale(hwnd)

	info, ierr := pageWindowMetrics(b)
	if ierr != nil {
		return osScale, nil
	}
	rect, rerr := browserWindowRect(b)
	if rerr != nil {
		return osScale, nil
	}
	return resolveWindowScale(info, rect, osScale, b.warnf), nil
}

// warnf logs at Warn level, tolerating a Browser assembled without a logger.
func (b *Browser) warnf(format string, args ...any) {
	if b.log != nil {
		b.log.Warnf(format, args...)
	}
}

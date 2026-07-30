//go:build windows

package input

import (
	"fmt"
	"syscall"
	"unsafe"
)

// ChromeWindowClass is the Win32 window class Chrome uses for its top-level
// browser windows. Mirrors firefoxkit's MozillaWindowClass so both kits expose
// the same shape.
const ChromeWindowClass = "Chrome_WidgetWin_1"

var (
	offsetUser32        = syscall.NewLazyDLL("user32.dll")
	procGetDpiForWindow = offsetUser32.NewProc("GetDpiForWindow")
	procGetWindowRect   = offsetUser32.NewProc("GetWindowRect")
)

// Seams over the two Win32 measurements. Production points them at the real
// syscalls; tests replace them so DPI and window geometry are inputs rather
// than properties of whatever machine happens to run the suite.
var (
	dpiForWindow  = rawDpiForWindow
	windowRectFor = rawWindowRect
)

// MeasureDPIScale returns the per-monitor DPI scale factor for the window
// (1.0 at 96 DPI, 1.25 at 120, 1.5 at 144). Used so CSS pixels map to the right
// number of physical screen pixels under display scaling.
//
// The kit measures this itself rather than accepting it from the caller: a
// scale the caller decided up front and the scale the window is actually
// rendered at are two different numbers the moment a window is dragged to
// another monitor.
//
// Falls back to 1.0 when GetDpiForWindow is unavailable (pre-Windows 10 1607)
// or reports 0.
func MeasureDPIScale(hwnd uintptr) float64 {
	dpi, ok := dpiForWindow(hwnd)
	if !ok || dpi == 0 {
		return 1.0
	}
	return float64(dpi) / 96.0
}

// MeasureWindowRect returns the window's outer rectangle in absolute screen
// pixels. Out-of-band by construction — GetWindowRect answers to the window
// manager, not to anything running inside the page — which is what lets it
// bound an offset measured by page script.
func MeasureWindowRect(hwnd uintptr) (Rect, error) {
	return windowRectFor(hwnd)
}

func rawDpiForWindow(hwnd uintptr) (uint32, bool) {
	if err := procGetDpiForWindow.Find(); err != nil {
		return 0, false
	}
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	return uint32(dpi), true
}

func rawWindowRect(hwnd uintptr) (Rect, error) {
	var r struct{ Left, Top, Right, Bottom int32 }
	ret, _, e := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return Rect{}, fmt.Errorf("GetWindowRect: %v", e)
	}
	return Rect{Left: int(r.Left), Top: int(r.Top), Right: int(r.Right), Bottom: int(r.Bottom)}, nil
}

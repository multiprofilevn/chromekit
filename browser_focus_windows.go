//go:build windows

package chromekit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/chromedp/chromedp"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procIsIconic                 = user32.NewProc("IsIconic")
	procKeybdEvent               = user32.NewProc("keybd_event")
)

const (
	swRestore         = 9
	asfwAny           = 0xFFFFFFFF
	chromeWindowClass = "Chrome_WidgetWin_1"
	vkMenu            = 0x12
	keyEventfKeyUp    = 0x0002
)

// SetHWND lets an external orchestrator (e.g. chromefleet) inject a window
// handle it discovered via PID, bypassing the title-substring heuristic.
func (b *Browser) SetHWND(hwnd uintptr) {
	b.hwndMu.Lock()
	defer b.hwndMu.Unlock()
	b.cachedHWND = hwnd
}

// HWND returns the Win32 handle for the Chrome window owning the current page.
//
// Resolution order:
//  1. Explicit SetHWND value (cached).
//  2. Enumerate top-level windows with class "Chrome_WidgetWin_1", visible,
//     whose title contains the current page title (case-insensitive).
//
// Returns an error if zero or multiple windows match the heuristic.
func (b *Browser) HWND() (uintptr, error) {
	b.hwndMu.Lock()
	cached := b.cachedHWND
	b.hwndMu.Unlock()
	if cached != 0 {
		return cached, nil
	}

	title, err := b.currentPageTitle()
	if err != nil {
		return 0, fmt.Errorf("hwnd: read page title: %w", err)
	}
	if strings.TrimSpace(title) == "" {
		return 0, errors.New("hwnd: current page has empty title — navigate first or call SetHWND")
	}

	matches, err := findChromeWindowsByTitle(title)
	if err != nil {
		return 0, err
	}
	switch len(matches) {
	case 0:
		return 0, fmt.Errorf("hwnd: no Chrome window matching title %q", title)
	case 1:
		b.hwndMu.Lock()
		b.cachedHWND = matches[0]
		b.hwndMu.Unlock()
		return matches[0], nil
	default:
		return 0, fmt.Errorf("hwnd: %d Chrome windows match title %q — call SetHWND", len(matches), title)
	}
}

// Focus brings the Chrome window to the foreground and waits until the OS
// confirms the switch. Windows blocks SetForegroundWindow when the calling
// process has not had foreground recently; mitigated via
// AllowSetForegroundWindow + an ALT-tap retry.
func (b *Browser) Focus(ctx context.Context) error {
	hwnd, err := b.HWND()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	procAllowSetForegroundWindow.Call(uintptr(asfwAny))

	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		procShowWindow.Call(hwnd, uintptr(swRestore))
	}
	procSetForegroundWindow.Call(hwnd)

	if foregroundIs(hwnd) {
		return nil
	}

	// Retry: tap ALT to grant our thread foreground permission, then retry.
	procKeybdEvent.Call(uintptr(vkMenu), 0, 0, 0)
	procKeybdEvent.Call(uintptr(vkMenu), 0, uintptr(keyEventfKeyUp), 0)
	procSetForegroundWindow.Call(hwnd)

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if foregroundIs(hwnd) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("focus: SetForegroundWindow refused (hwnd=0x%x)", hwnd)
}

func foregroundIs(hwnd uintptr) bool {
	cur, _, _ := procGetForegroundWindow.Call()
	return cur == hwnd
}

func (b *Browser) currentPageTitle() (string, error) {
	p := b.Current()
	if p == nil {
		return "", ErrPageNotFound
	}
	ctx, cancel := context.WithTimeout(b.ctx, 2*time.Second)
	defer cancel()
	var title string
	if err := chromedp.Run(ctx, chromedp.Title(&title)); err != nil {
		return "", err
	}
	return title, nil
}

func findChromeWindowsByTitle(needle string) ([]uintptr, error) {
	needleLower := strings.ToLower(needle)
	var matches []uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if !classNameEquals(hwnd, chromeWindowClass) {
			return 1
		}
		title := windowText(hwnd)
		if title == "" {
			return 1
		}
		if strings.Contains(strings.ToLower(title), needleLower) {
			matches = append(matches, hwnd)
		}
		return 1
	})
	ret, _, errno := procEnumWindows.Call(cb, 0)
	if ret == 0 && len(matches) == 0 {
		return nil, fmt.Errorf("EnumWindows: %v", errno)
	}
	return matches, nil
}

func classNameEquals(hwnd uintptr, want string) bool {
	buf := make([]uint16, 256)
	procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf) == want
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, int(n)+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

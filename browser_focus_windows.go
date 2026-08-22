//go:build windows

package chromekit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/chromedp/chromedp"
	"github.com/multiprofilevn/chromekit/input"
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
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")

	iphlpapi                = syscall.NewLazyDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	swRestore         = 9
	asfwAny           = 0xFFFFFFFF
	chromeWindowClass = "Chrome_WidgetWin_1"
	vkMenu            = 0x12
	keyEventfKeyUp    = 0x0002

	mibTcpStateListen          = 2
	tcpTableOwnerPidAll        = 5
	afInet              uint32 = 2
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
//  2. PID lookup — find chrome's process via b.port (Connect) or b.pid
//     (Launch), then EnumWindows filter by PID + class. Robust against
//     duplicate titles, async title propagation, Unicode mismatches.
//  3. Title-based fallback — enumerate windows with class
//     "Chrome_WidgetWin_1" whose title contains the current page title.
//     Kept as defense-in-depth for cases where PID lookup fails.
func (b *Browser) HWND() (uintptr, error) {
	pid := b.resolvePID()

	b.hwndMu.Lock()
	cached := b.cachedHWND
	b.hwndMu.Unlock()
	// Validate before trusting the cache: the window may have closed, and
	// Windows recycles HWND values, so a stale handle can silently resolve to
	// an unrelated application — native input would then type into it.
	if cached != 0 {
		if isVisibleChromeWindow(cached) {
			return cached, nil
		}
		b.hwndMu.Lock()
		if b.cachedHWND == cached {
			b.cachedHWND = 0
		}
		b.hwndMu.Unlock()
	}

	if pid > 0 {
		if hwnd, err := findChromeWindowByPID(pid); err == nil {
			b.hwndMu.Lock()
			b.cachedHWND = hwnd
			b.hwndMu.Unlock()
			return hwnd, nil
		}
	}

	title, err := b.currentPageTitle()
	if err != nil {
		return 0, fmt.Errorf("hwnd: read page title: %w", err)
	}
	if strings.TrimSpace(title) == "" {
		return 0, errors.New("hwnd: current page has empty title — navigate first or call SetHWND")
	}

	// Titles come from the page, so any site can pick one. With several
	// browsers on one machine a page can name itself after another instance's
	// tab and steal its native input; scoping to our own process closes that.
	// With no PID available the match stays global — documented limitation.
	matches, err := findChromeWindowsByTitleOwnedBy(title, pid)
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

// resolvePID returns the chrome browser process PID for HWND lookup. Prefers
// the Launch-stored PID; falls back to looking up the Connect port owner.
// Returns 0 when neither source is available.
func (b *Browser) resolvePID() uint32 {
	if b.pid != 0 {
		return b.pid
	}
	if b.port == 0 {
		return 0
	}
	pid, err := lookupListeningPID(b.port)
	if err != nil {
		return 0
	}
	return pid
}

// Focus brings the Chrome window to the foreground and waits until the OS
// confirms the switch. Windows blocks SetForegroundWindow when the calling
// process has not had foreground recently; mitigated via
// AllowSetForegroundWindow + an ALT-tap retry.
//
// Before HWND lookup, Focus activates the current tab via CDP so the OS title
// bar reflects this page's title — title-substring HWND lookup depends on it.
// HWND lookup itself retries up to 500ms because Chrome → Win32 title
// propagation is asynchronous (typically 50-200ms after activateTarget).
func (b *Browser) Focus(ctx context.Context) error {
	if p := b.Current(); p != nil {
		// Fatal, not warn-and-continue: if the tab isn't confirmed active,
		// native input would land on the wrong tab (the recurring bug). Fail
		// the focus so the job fails loud instead of clicking blind.
		if err := p.BringToFront(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	hwnd, err := b.lookupHWNDWithRetry(ctx, 500*time.Millisecond, 100*time.Millisecond)
	if err != nil {
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
	// Serialized with every other native input op — an unsynchronized ALT tap
	// lands in the middle of another browser's click or key combo when several
	// browsers share the machine, and the OS applies it to whatever has focus.
	input.SerializeNativeInput(func() {
		procKeybdEvent.Call(uintptr(vkMenu), 0, 0, 0)
		procKeybdEvent.Call(uintptr(vkMenu), 0, uintptr(keyEventfKeyUp), 0)
	})
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

// lookupHWNDWithRetry calls b.HWND() and retries on the "no Chrome window
// matching title" error up to budget, sleeping step between attempts. Other
// errors return immediately. Respects ctx cancellation.
func (b *Browser) lookupHWNDWithRetry(ctx context.Context, budget, step time.Duration) (uintptr, error) {
	deadline := time.Now().Add(budget)
	attempts := 0
	for {
		attempts++
		hwnd, err := b.HWND()
		if err == nil {
			return hwnd, nil
		}
		if !strings.Contains(err.Error(), "no Chrome window matching") {
			return 0, err
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("focus: title not visible after %v (retried %d): %w", budget, attempts, err)
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(step):
		}
	}
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

// enumWindowsCB is allocated exactly once, at package init.
//
// syscall.NewCallback never releases its slot and Go only guarantees 1024 of
// them per process, so building one per enumeration panics with "too many
// callback functions" once a long-running fleet has done enough HWND lookups.
// The Win32 callback signature has no free parameter to carry per-call state
// (EnumWindows' lParam is spent on the callback itself), hence the package
// vars below plus enumMu to serialize. Enumeration only runs on HWND cache
// misses, so serializing costs nothing on the hot path.
var (
	enumMu      sync.Mutex
	enumFilter  func(hwnd uintptr) bool
	enumMatches []uintptr

	enumWindowsCB = syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if enumFilter(hwnd) {
			enumMatches = append(enumMatches, hwnd)
		}
		return 1
	})
)

// enumChromeWindows runs EnumWindows and returns every window filter accepts.
func enumChromeWindows(filter func(hwnd uintptr) bool) ([]uintptr, error) {
	enumMu.Lock()
	defer enumMu.Unlock()

	enumFilter = filter
	enumMatches = enumMatches[:0]
	ret, _, errno := procEnumWindows.Call(enumWindowsCB, 0)
	out := make([]uintptr, len(enumMatches))
	copy(out, enumMatches)
	enumFilter = nil

	if ret == 0 && len(out) == 0 {
		return nil, fmt.Errorf("EnumWindows: %v", errno)
	}
	return out, nil
}

// isVisibleChromeWindow is the shared precondition of both lookups: a visible
// top-level window of Chrome's own window class.
func isVisibleChromeWindow(hwnd uintptr) bool {
	if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
		return false
	}
	return classNameEquals(hwnd, chromeWindowClass)
}

// titleMatches reports whether a window title contains needle, case-insensitively.
// Untitled windows never match — a blank title carries no identity.
func titleMatches(title, needle string) bool {
	if title == "" {
		return false
	}
	return strings.Contains(strings.ToLower(title), strings.ToLower(needle))
}

func findChromeWindowsByTitle(needle string) ([]uintptr, error) {
	return findChromeWindowsByTitleOwnedBy(needle, 0)
}

// findChromeWindowsByTitleOwnedBy matches on title, restricted to windows
// owned by pid when pid is non-zero.
func findChromeWindowsByTitleOwnedBy(needle string, pid uint32) ([]uintptr, error) {
	return enumChromeWindows(func(hwnd uintptr) bool {
		if !isVisibleChromeWindow(hwnd) {
			return false
		}
		if pid != 0 && windowOwnerPID(hwnd) != pid {
			return false
		}
		return titleMatches(windowText(hwnd), needle)
	})
}

// windowOwnerPID returns the process id that owns hwnd.
func windowOwnerPID(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func classNameEquals(hwnd uintptr, want string) bool {
	buf := make([]uint16, 256)
	procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf) == want
}

// findChromeWindowByPID enumerates top-level windows and returns the visible
// Chrome_WidgetWin_1 window owned by pid. Returns an error if 0 or multiple
// windows match (multi case = browser opened a second window from same proc;
// caller should SetHWND explicitly).
func findChromeWindowByPID(pid uint32) (uintptr, error) {
	matches, err := enumChromeWindows(func(hwnd uintptr) bool {
		if !isVisibleChromeWindow(hwnd) {
			return false
		}
		return windowOwnerPID(hwnd) == pid
	})
	if err != nil {
		return 0, err
	}
	switch len(matches) {
	case 0:
		return 0, fmt.Errorf("hwnd: no Chrome window for pid %d", pid)
	case 1:
		return matches[0], nil
	default:
		// Multi-window for same PID: most common cause is Chrome's hidden
		// helper windows (e.g. tooltip overlay) sharing the class. Pick the
		// one with a non-empty title — that's the user-visible browser
		// window. If still ambiguous, return the first.
		for _, h := range matches {
			if windowText(h) != "" {
				return h, nil
			}
		}
		return matches[0], nil
	}
}

// mibTcpRowOwnerPid mirrors MIB_TCPROW_OWNER_PID. Local/remote port and addr
// are stored network byte order in the low 16 bits of a uint32.
type mibTcpRowOwnerPid struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPid  uint32
}

// lookupListeningPID returns the PID owning the IPv4 TCP listening socket on
// the given local port (loopback or otherwise — Chrome's debug port binds
// 127.0.0.1 only). Two-pass: size probe, then fetch.
func lookupListeningPID(port int) (uint32, error) {
	if port <= 0 || port > 0xFFFF {
		return 0, fmt.Errorf("lookupListeningPID: bad port %d", port)
	}
	wantPort := uint32(((port & 0xFF) << 8) | ((port >> 8) & 0xFF)) // htons in low 16 bits

	var size uint32
	// First call gets required buffer size into `size`.
	procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(afInet), uintptr(tcpTableOwnerPidAll), 0)
	if size == 0 {
		return 0, errors.New("lookupListeningPID: empty TCP table")
	}
	buf := make([]byte, size)
	ret, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(afInet), uintptr(tcpTableOwnerPidAll), 0)
	if ret != 0 {
		return 0, fmt.Errorf("GetExtendedTcpTable: code=%d", ret)
	}
	dwNumEntries := *(*uint32)(unsafe.Pointer(&buf[0]))
	rowSize := unsafe.Sizeof(mibTcpRowOwnerPid{})
	for i := uint32(0); i < dwNumEntries; i++ {
		row := (*mibTcpRowOwnerPid)(unsafe.Pointer(&buf[4+uintptr(i)*rowSize]))
		if row.State == mibTcpStateListen && row.LocalPort == wantPort {
			return row.OwningPid, nil
		}
	}
	return 0, fmt.Errorf("lookupListeningPID: port %d not in listening TCP table", port)
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

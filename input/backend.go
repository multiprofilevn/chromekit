// Package input contains the InputBackend interface and its implementations
// (CDP via chromedp + Native via winapi). Kept separate so chromekit's root
// package can swap backends at runtime without depending on platform code.
package input

import (
	"context"
	"errors"
)

// ErrNotSupported signals the active backend cannot perform the call on this
// platform (e.g. native backend on macOS/Linux).
var ErrNotSupported = errors.New("input: not supported on this platform")

// ErrForegroundLost signals the target window no longer owns the OS foreground,
// so the next keystroke would land in whatever window took it. Emission stops
// at once — SendInput is not addressed to a window, so continuing would type
// into a foreign application.
var ErrForegroundLost = errors.New("input: foreground lost")

// GuardChunkSize is how many characters may be emitted between two
// ForegroundGuard checks. It bounds the leak: losing the foreground mid-string
// can spill at most this many keystrokes into a foreign window, in exchange for
// keeping the check off the per-keystroke path.
const GuardChunkSize = 5

// MouseButton enumerates the buttons a backend understands.
type MouseButton int

const (
	MouseLeft MouseButton = iota
	MouseRight
	MouseMiddle
)

// Type selects a backend implementation.
type Type int

const (
	TypeCDP Type = iota
	TypeNative
)

// Window describes where the Chrome window sits on screen, so DOM coordinates
// can be converted to absolute screen pixels by the native backend.
//
// X, Y are the window's top-left in screen pixels (matches Chrome's
// --window-position flag). ContentOffsetX/Y are the chrome-chrome offset
// (title bar + tabs + omnibox), so CSS (0,0) — the page content origin —
// maps to screen (X + ContentOffsetX, Y + ContentOffsetY). Scale is the OS
// DPI factor applied to CSS coords before the offsets are added.
type Window struct {
	X, Y           int
	Scale          float64
	ContentOffsetX int
	ContentOffsetY int
}

// Rect is a window rectangle in absolute screen pixels, as reported by Win32
// GetWindowRect. It is an OUT-OF-BAND measurement: it comes from the window
// manager, not from anything the page can script, which is what makes it usable
// as a sanity bound on offsets measured inside the page realm.
type Rect struct {
	Left, Top, Right, Bottom int
}

// Config bundles knobs the backends need at construction time.
type Config struct {
	Window Window
	// ForegroundGuard is consulted by the lowest-level emitters (sendKey,
	// sendVK, sendMouse) before input reaches the OS. A non-nil return aborts
	// the emission and propagates to the caller; return ErrForegroundLost when
	// the target window no longer owns the foreground. nil disables the check.
	//
	// Placing it at the emitters rather than at Keyboard means ClearInput,
	// Combo, Press, Type and TypeHuman are covered structurally — no call site
	// has to remember. Keyboard emissions are checked once per GuardChunkSize
	// characters; mouse button events are checked every time (they are rare).
	ForegroundGuard func() error
}

// Modifier is a bitmask of modifier keys for KeyCombo. Compose with bitwise
// OR (e.g. ModCtrl | ModShift). Pass 0 for an unmodified key.
type Modifier uint32

const (
	ModCtrl Modifier = 1 << iota // Control / Cmd-equivalent
	ModAlt                       // Alt / Option
	ModShift
	ModWin // Windows / Super
)

// Virtual-key codes for the most common non-character keys used by KeyCombo.
// Match Win32 VK_* values — the native backend forwards them as-is.
const (
	VKReturn    uint16 = 0x0D
	VKBackspace uint16 = 0x08
	VKEscape    uint16 = 0x1B
	VKEnd       uint16 = 0x23
	VKHome      uint16 = 0x24
	VKDelete    uint16 = 0x2E
	VKA         uint16 = 0x41
	VKL         uint16 = 0x4C
)

// Backend is the low-level mouse/keyboard dispatcher. Higher-level helpers
// (Cursor, Keyboard, TouchScreen) layer human-like behavior on top.
type Backend interface {
	MouseMove(ctx context.Context, x, y float64) error
	MouseDown(ctx context.Context, x, y float64, button MouseButton) error
	MouseUp(ctx context.Context, x, y float64, button MouseButton) error
	MouseScroll(ctx context.Context, deltaX, deltaY float64) error
	KeyChar(ctx context.Context, ch rune) error
	InsertText(ctx context.Context, text string) error
	// KeyCombo presses a virtual-key code with optional modifiers held down,
	// then releases everything. Use for shortcuts (Ctrl+L, Ctrl+A) and
	// non-character keys (Enter, End, Esc) that KeyChar can't dispatch.
	// Returns ErrNotSupported on backends that lack OS keyboard access (CDP,
	// native on macOS/Linux today).
	KeyCombo(ctx context.Context, vk uint16, mods Modifier) error
	// SetWindow replaces the coordinate frame this backend maps CSS pixels
	// through, IN PLACE. It exists so a re-measured frame can reach a backend
	// that is already wired into a Cursor / Keyboard / TouchScreen without
	// replacing the backend object: ~30 call sites read the backend pointer
	// bare, and swapping it between a MouseDown and its MouseUp leaves the
	// button held down at the OS level forever.
	//
	// Implementations MUST be safe to call while another goroutine is mid-op
	// and MUST NOT block that op for meaningfully long. Backends that dispatch
	// over the wire (CDP) have no screen frame and no-op.
	SetWindow(w Window)
}

// New returns a backend by type. Native on unsupported platforms returns a
// stub whose methods all error with ErrNativeNotSupported — caller decides
// whether to fall back to CDP.
func New(t Type, cfg Config) Backend {
	switch t {
	case TypeNative:
		return newNative(cfg)
	default:
		return newCDP()
	}
}

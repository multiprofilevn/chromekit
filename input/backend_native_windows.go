//go:build windows

package input

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// nativeInputMu serializes every raw OS input op (SendInput / SetCursorPos)
// so direct chromekit users with multiple Browsers can't interleave half a
// click with another's keystroke. chromefleet additionally serializes at the
// fleet level, making this a no-op in that case — leaving it in keeps the
// safety net for callers who use chromekit standalone.
var nativeInputMu sync.Mutex

const (
	inputMouse    uint32 = 0
	inputKeyboard uint32 = 1

	mouseEventfMove      uint32 = 0x0001
	mouseEventfLeftDown  uint32 = 0x0002
	mouseEventfLeftUp    uint32 = 0x0004
	mouseEventfRightDown uint32 = 0x0008
	mouseEventfRightUp   uint32 = 0x0010
	mouseEventfMidDown   uint32 = 0x0020
	mouseEventfMidUp     uint32 = 0x0040
	mouseEventfWheel     uint32 = 0x0800
	mouseEventfHWheel    uint32 = 0x01000

	keyEventfKeyUp   uint32 = 0x0002
	keyEventfUnicode uint32 = 0x0004

	wheelDelta = 120
)

type mouseInput struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	_           uint32
	DwExtraInfo uintptr
}

type mouseInputRaw struct {
	Type uint32
	_    [4]byte
	Mi   mouseInput
}

type keyboardInput struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	_           uint32
	DwExtraInfo uintptr
}

type keyboardInputRaw struct {
	Type uint32
	_    [4]byte
	Ki   keyboardInput
	_    [8]byte
}

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procSendInput    = user32.NewProc("SendInput")
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procGetCursorPos = user32.NewProc("GetCursorPos")
)

type nativeBackend struct {
	win Window
}

func newNative(cfg Config) Backend {
	w := cfg.Window
	if w.Scale <= 0 {
		w.Scale = 1.0
	}
	return &nativeBackend{win: w}
}

func (n *nativeBackend) toScreen(x, y float64) (int32, int32) {
	sx, sy := cssToScreen(x, y, n.win)
	return int32(math.Floor(sx)), int32(math.Floor(sy))
}

// cssToScreen / screenToCSS are pure inverse transforms between CSS
// content-origin coords and absolute screen pixels. Window.X/Y is the
// window's screen top-left; ContentOffsetX/Y is the chrome-chrome offset
// (title bar + tabs + omnibox) in raw screen pixels (NOT scaled). CSS
// coords are content-relative and get DPI-scaled.
//
// Invariant: screenToCSS(cssToScreen(x, y, win), win) == (x, y) — exact
// modulo the int32 floor in toScreen. Asymmetry between these two has
// caused visible cursor "teleport-back-then-glide" artefacts (the
// chromefleet click sequence calls MoveTo twice; the second one seeds its
// Bezier from OSPositionCSS, so a broken inverse leaks ~ContentOffset/Scale
// pixels of phantom drift into the path).
func cssToScreen(x, y float64, win Window) (float64, float64) {
	scale := win.Scale
	if scale <= 0 {
		scale = 1.0
	}
	sx := float64(win.X+win.ContentOffsetX) + x*scale
	sy := float64(win.Y+win.ContentOffsetY) + y*scale
	return sx, sy
}

func screenToCSS(px, py float64, win Window) (float64, float64) {
	scale := win.Scale
	if scale <= 0 {
		scale = 1.0
	}
	cssX := (px - float64(win.X+win.ContentOffsetX)) / scale
	cssY := (py - float64(win.Y+win.ContentOffsetY)) / scale
	return cssX, cssY
}

// posPoint mirrors Win32 POINT.
type posPoint struct{ X, Y int32 }

// OSPositionCSS returns the current OS cursor position translated into this
// backend's CSS coordinate space (relative to the browser window's content
// origin). MUST be the inverse of toScreen — both subtract Window.X/Y AND
// ContentOffsetX/Y, then divide by Scale. If ContentOffset is dropped here,
// the next MoveTo seeds its Bezier from a point ~ContentOffset/Scale px
// away from where the cursor actually is → the OS cursor teleports forward
// on the first sub-step, then animates back to target. Visible jitter on
// the second MoveTo of any click sequence (MoveTo then ClickAt → Click
// internally calls MoveTo again).
func (n *nativeBackend) OSPositionCSS() (float64, float64, bool) {
	var p posPoint
	ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	if ok == 0 {
		return 0, 0, false
	}
	cssX, cssY := screenToCSS(float64(p.X), float64(p.Y), n.win)
	return cssX, cssY, true
}

func (n *nativeBackend) MouseMove(ctx context.Context, x, y float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sx, sy := n.toScreen(x, y)
	nativeInputMu.Lock()
	defer nativeInputMu.Unlock()
	procSetCursorPos.Call(uintptr(sx), uintptr(sy))
	return nil
}

func (n *nativeBackend) MouseDown(ctx context.Context, x, y float64, b MouseButton) error {
	if err := n.MouseMove(ctx, x, y); err != nil {
		return err
	}
	flag := mouseDownFlag(b)
	return sendMouse(flag, 0)
}

func (n *nativeBackend) MouseUp(ctx context.Context, x, y float64, b MouseButton) error {
	if err := n.MouseMove(ctx, x, y); err != nil {
		return err
	}
	flag := mouseUpFlag(b)
	return sendMouse(flag, 0)
}

func (n *nativeBackend) MouseScroll(ctx context.Context, deltaX, deltaY float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deltaY != 0 {
		amount := uint32(int32(-deltaY * float64(wheelDelta)))
		if err := sendMouse(mouseEventfWheel, amount); err != nil {
			return err
		}
	}
	if deltaX != 0 {
		amount := uint32(int32(deltaX * float64(wheelDelta)))
		return sendMouse(mouseEventfHWheel, amount)
	}
	return nil
}

func (n *nativeBackend) KeyChar(ctx context.Context, ch rune) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sendKey(uint16(ch), keyEventfUnicode); err != nil {
		return err
	}
	time.Sleep(time.Duration(40+rand.Intn(40)) * time.Millisecond)
	return sendKey(uint16(ch), keyEventfUnicode|keyEventfKeyUp)
}

func (n *nativeBackend) InsertText(ctx context.Context, text string) error {
	for _, ch := range text {
		if err := n.KeyChar(ctx, ch); err != nil {
			return err
		}
		time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
	}
	return nil
}

// vkForModifier maps a Modifier bit to its Win32 virtual-key code.
const (
	vkControl uint16 = 0x11
	vkMenuKey uint16 = 0x12 // VK_MENU = ALT (renamed to avoid clash)
	vkShift   uint16 = 0x10
	vkLWin    uint16 = 0x5B
)

func (n *nativeBackend) KeyCombo(ctx context.Context, vk uint16, mods Modifier) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Walk the bits in a stable order (Ctrl, Alt, Shift, Win) so press/
	// release pair up symmetrically.
	mappings := []struct {
		bit Modifier
		key uint16
	}{
		{ModCtrl, vkControl},
		{ModAlt, vkMenuKey},
		{ModShift, vkShift},
		{ModWin, vkLWin},
	}
	var held []uint16
	for _, m := range mappings {
		if mods&m.bit != 0 {
			held = append(held, m.key)
		}
	}

	for _, k := range held {
		if err := sendVK(k, 0); err != nil {
			return err
		}
		time.Sleep(time.Duration(20+rand.Intn(20)) * time.Millisecond)
	}
	if err := sendVK(vk, 0); err != nil {
		return err
	}
	time.Sleep(time.Duration(40+rand.Intn(40)) * time.Millisecond)
	if err := sendVK(vk, keyEventfKeyUp); err != nil {
		return err
	}
	time.Sleep(time.Duration(20+rand.Intn(20)) * time.Millisecond)
	for i := len(held) - 1; i >= 0; i-- {
		if err := sendVK(held[i], keyEventfKeyUp); err != nil {
			return err
		}
		time.Sleep(time.Duration(20+rand.Intn(20)) * time.Millisecond)
	}
	return nil
}

// sendVK sends one keyboard event by virtual-key code (WVk field), without
// the Unicode flag — use for modifier keys + non-character VK like Enter,
// End, Esc, F-keys. flags = 0 for down, keyEventfKeyUp for up.
func sendVK(vk uint16, flags uint32) error {
	nativeInputMu.Lock()
	defer nativeInputMu.Unlock()
	in := keyboardInputRaw{
		Type: inputKeyboard,
		Ki:   keyboardInput{WVk: vk, DwFlags: flags},
	}
	ret, _, errno := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if ret != 1 {
		return errno
	}
	return nil
}

func mouseDownFlag(b MouseButton) uint32 {
	switch b {
	case MouseRight:
		return mouseEventfRightDown
	case MouseMiddle:
		return mouseEventfMidDown
	default:
		return mouseEventfLeftDown
	}
}

func mouseUpFlag(b MouseButton) uint32 {
	switch b {
	case MouseRight:
		return mouseEventfRightUp
	case MouseMiddle:
		return mouseEventfMidUp
	default:
		return mouseEventfLeftUp
	}
}

func sendMouse(flags, data uint32) error {
	nativeInputMu.Lock()
	defer nativeInputMu.Unlock()
	in := mouseInputRaw{
		Type: inputMouse,
		Mi:   mouseInput{DwFlags: flags, MouseData: data},
	}
	ret, _, errno := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if ret != 1 {
		return errno
	}
	return nil
}

func sendKey(scan uint16, flags uint32) error {
	nativeInputMu.Lock()
	defer nativeInputMu.Unlock()
	in := keyboardInputRaw{
		Type: inputKeyboard,
		Ki:   keyboardInput{WScan: scan, DwFlags: flags},
	}
	ret, _, errno := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if ret != 1 {
		return errno
	}
	return nil
}

var _ Backend = (*nativeBackend)(nil)

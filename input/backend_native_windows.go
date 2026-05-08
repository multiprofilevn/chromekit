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
	user32       = syscall.NewLazyDLL("user32.dll")
	procSendInput   = user32.NewProc("SendInput")
	procSetCursorPos = user32.NewProc("SetCursorPos")
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
	sx := int32(math.Floor(float64(n.win.X) + x*n.win.Scale))
	sy := int32(math.Floor(float64(n.win.Y) + y*n.win.Scale))
	return sx, sy
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

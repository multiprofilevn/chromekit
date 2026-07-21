package input

import (
	"context"
	"fmt"
	"time"

	cdpinput "github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// vkSpec is the CDP-side payload for a virtual-key code: the DOM `key`,
// physical `code`, optional `text` produced when typed unmodified, and the
// numeric VK echoed back via WithWindowsVirtualKeyCode (lets page-side
// handlers that read `event.keyCode` work).
type vkSpec struct {
	key    string
	code   string
	text   string
	vkCode int64
}

// vkToCDPKey maps the package's VK_* constants (input/backend.go) to the CDP
// fields needed for DispatchKeyEvent. Adding a new VK constant without an
// entry here will cause cdpBackend.KeyCombo to return an "unsupported vk"
// error — by design, so the mapping stays explicit.
var vkToCDPKey = map[uint16]vkSpec{
	VKReturn:    {key: "Enter", code: "Enter", text: "\r", vkCode: 13},
	VKBackspace: {key: "Backspace", code: "Backspace", vkCode: 8},
	VKEscape:    {key: "Escape", code: "Escape", vkCode: 27},
	VKEnd:       {key: "End", code: "End", vkCode: 35},
	VKHome:      {key: "Home", code: "Home", vkCode: 36},
	VKDelete:    {key: "Delete", code: "Delete", vkCode: 46},
	VKA:         {key: "a", code: "KeyA", text: "a", vkCode: 65},
	VKL:         {key: "l", code: "KeyL", text: "l", vkCode: 76},
}

// cdpModBits translates the package's Modifier bitmask into CDP's modifier
// ordering (Alt=1, Ctrl=2, Meta=4, Shift=8). Mirrors combineMods in
// page_keyboard_api.go but kept local to avoid an import cycle (chromekit
// depends on input, not the other way around).
func cdpModBits(m Modifier) cdpinput.Modifier {
	var out cdpinput.Modifier
	if m&ModAlt != 0 {
		out |= 1
	}
	if m&ModCtrl != 0 {
		out |= 2
	}
	if m&ModWin != 0 {
		out |= 4
	}
	if m&ModShift != 0 {
		out |= 8
	}
	return out
}

// perDispatchTimeout bounds a single CDP input dispatch.
//
// Input.dispatchMouseEvent and friends only acknowledge once the renderer has
// consumed the event, so a busy, throttled or wedged tab can leave the call
// waiting forever. A human-like gesture is dozens of these in a row: without a
// per-event bound one stuck renderer parks the whole gesture, and every other
// caller sharing that connection behind it. Five seconds is far longer than a
// healthy dispatch (sub-millisecond) and short enough to fail loud.
const perDispatchTimeout = 5 * time.Second

// dispatch runs one CDP input action under perDispatchTimeout.
func dispatch(ctx context.Context, action chromedp.Action) error {
	ctx, cancel := context.WithTimeout(ctx, perDispatchTimeout)
	defer cancel()
	return chromedp.Run(ctx, action)
}

type cdpBackend struct{}

func newCDP() Backend { return &cdpBackend{} }

func (cdpBackend) MouseMove(ctx context.Context, x, y float64) error {
	return dispatch(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpinput.DispatchMouseEvent(cdpinput.MouseMoved, x, y).Do(ctx)
	}))
}

func (cdpBackend) MouseDown(ctx context.Context, x, y float64, button MouseButton) error {
	return dispatch(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpinput.DispatchMouseEvent(cdpinput.MousePressed, x, y).
			WithButton(toCDPButton(button)).
			WithClickCount(1).
			Do(ctx)
	}))
}

func (cdpBackend) MouseUp(ctx context.Context, x, y float64, button MouseButton) error {
	return dispatch(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpinput.DispatchMouseEvent(cdpinput.MouseReleased, x, y).
			WithButton(toCDPButton(button)).
			WithClickCount(1).
			Do(ctx)
	}))
}

func (cdpBackend) MouseScroll(ctx context.Context, deltaX, deltaY float64) error {
	return dispatch(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpinput.DispatchMouseEvent(cdpinput.MouseWheel, 0, 0).
			WithDeltaX(deltaX).
			WithDeltaY(deltaY).
			Do(ctx)
	}))
}

func (cdpBackend) KeyChar(ctx context.Context, ch rune) error {
	s := string(ch)
	return dispatch(ctx, chromedp.KeyEvent(s))
}

func (cdpBackend) InsertText(ctx context.Context, text string) error {
	return dispatch(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpinput.InsertText(text).Do(ctx)
	}))
}

// KeyCombo dispatches a DOM key event (down + up) for vk with mods held. The
// target is the page DOM, not Chrome's chrome — flows that need to drive the
// omnibox or other Chrome UI must use the native backend (see Browser.Focus).
// Unknown vk → explicit error rather than silent no-op.
func (cdpBackend) KeyCombo(ctx context.Context, vk uint16, mods Modifier) error {
	spec, ok := vkToCDPKey[vk]
	if !ok {
		return fmt.Errorf("input/cdp: unsupported vk: 0x%X", vk)
	}
	mod := cdpModBits(mods)
	return dispatch(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		down := cdpinput.DispatchKeyEvent(cdpinput.KeyDown).
			WithKey(spec.key).
			WithCode(spec.code).
			WithWindowsVirtualKeyCode(spec.vkCode).
			WithModifiers(mod)
		// Only attach text when no modifier is held — for shortcuts (Ctrl+A,
		// Ctrl+L) the text would otherwise be inserted into the focused input
		// in addition to triggering the accelerator.
		if spec.text != "" && mods == 0 {
			down = down.WithText(spec.text)
		}
		if err := down.Do(ctx); err != nil {
			return err
		}
		return cdpinput.DispatchKeyEvent(cdpinput.KeyUp).
			WithKey(spec.key).
			WithCode(spec.code).
			WithWindowsVirtualKeyCode(spec.vkCode).
			WithModifiers(mod).
			Do(ctx)
	}))
}

func toCDPButton(b MouseButton) cdpinput.MouseButton {
	switch b {
	case MouseRight:
		return cdpinput.Right
	case MouseMiddle:
		return cdpinput.Middle
	default:
		return cdpinput.Left
	}
}

var _ Backend = (*cdpBackend)(nil)

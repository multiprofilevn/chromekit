package chromekit

import (
	"context"
	"fmt"

	cdpinput "github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/tuwibu/chromekit/input"
)

// KeyboardAPI groups keyboard primitives. Selector-based p.Type(sel, text)
// stays on Page as a high-level shortcut.
type KeyboardAPI struct{ p *Page }

// keySpec maps a chromekit key name to the CDP fields needed to dispatch a
// key event.
type keySpec struct {
	key  string
	code string
	vk   int64
	text string // optional text the keypress would produce (for printable keys)
}

var keyMap = map[string]keySpec{
	"Enter":      {key: "Enter", code: "Enter", vk: 13, text: "\r"},
	"Tab":        {key: "Tab", code: "Tab", vk: 9, text: "\t"},
	"Escape":     {key: "Escape", code: "Escape", vk: 27},
	"Backspace":  {key: "Backspace", code: "Backspace", vk: 8},
	"Delete":     {key: "Delete", code: "Delete", vk: 46},
	"ArrowUp":    {key: "ArrowUp", code: "ArrowUp", vk: 38},
	"ArrowDown":  {key: "ArrowDown", code: "ArrowDown", vk: 40},
	"ArrowLeft":  {key: "ArrowLeft", code: "ArrowLeft", vk: 37},
	"ArrowRight": {key: "ArrowRight", code: "ArrowRight", vk: 39},
	"Home":       {key: "Home", code: "Home", vk: 36},
	"End":        {key: "End", code: "End", vk: 35},
	"PageUp":     {key: "PageUp", code: "PageUp", vk: 33},
	"PageDown":   {key: "PageDown", code: "PageDown", vk: 34},
	"Space":      {key: " ", code: "Space", vk: 32, text: " "},
}

// Press dispatches keydown + keyup for key, holding mods for the duration.
// key accepts either a Chrome key name ("Enter", "Tab", "ArrowDown") or a
// single printable character ("a", "1", "?"). Pass input.ModCtrl etc. via
// variadic for shortcuts.
func (k *KeyboardAPI) Press(key string, mods ...input.Modifier) error {
	if err := k.p.alive(); err != nil {
		return err
	}
	mod := combineMods(mods)
	return chromedp.Run(k.p.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := dispatchKey(ctx, cdpinput.KeyDown, key, mod); err != nil {
			return err
		}
		return dispatchKey(ctx, cdpinput.KeyUp, key, mod)
	}))
}

// Down dispatches keydown without releasing. Pair with Up.
func (k *KeyboardAPI) Down(key string, mods ...input.Modifier) error {
	if err := k.p.alive(); err != nil {
		return err
	}
	return chromedp.Run(k.p.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return dispatchKey(ctx, cdpinput.KeyDown, key, combineMods(mods))
	}))
}

// Up dispatches keyup. Caller's responsibility to balance Down calls.
func (k *KeyboardAPI) Up(key string, mods ...input.Modifier) error {
	if err := k.p.alive(); err != nil {
		return err
	}
	return chromedp.Run(k.p.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return dispatchKey(ctx, cdpinput.KeyUp, key, combineMods(mods))
	}))
}

// Type sends each rune in text via the backend, sleeping a random delay
// between key presses for a human-ish cadence.
func (k *KeyboardAPI) Type(text string) error {
	if err := k.p.alive(); err != nil {
		return err
	}
	k.p.initInput()
	return k.p.keyboard.Type(k.p.ctx, text)
}

// TypeFast skips the random delay loop and asks the backend to bulk-insert.
// CDP backend pastes in one event; native backend still goes per-rune.
func (k *KeyboardAPI) TypeFast(text string) error {
	if err := k.p.alive(); err != nil {
		return err
	}
	k.p.initInput()
	return k.p.keyboard.TypeFast(k.p.ctx, text)
}

func dispatchKey(ctx context.Context, t cdpinput.KeyType, key string, mod int64) error {
	ev := cdpinput.DispatchKeyEvent(t).WithModifiers(cdpinput.Modifier(mod))
	if spec, ok := keyMap[key]; ok {
		ev = ev.WithKey(spec.key).WithCode(spec.code).WithWindowsVirtualKeyCode(spec.vk)
		if spec.text != "" && t == cdpinput.KeyDown {
			ev = ev.WithText(spec.text)
		}
	} else {
		ev = ev.WithKey(key)
		if t == cdpinput.KeyDown && len(key) == 1 {
			ev = ev.WithText(key)
		}
	}
	if err := ev.Do(ctx); err != nil {
		return fmt.Errorf("%w: dispatch key %q: %s", ErrEvaluate, key, err.Error())
	}
	return nil
}

// combineMods OR-folds chromekit input.Modifier flags into the CDP modifier
// bitmask. CDP order differs from chromekit's (1=Alt, 2=Ctrl, 4=Meta,
// 8=Shift) so we translate per-flag.
func combineMods(mods []input.Modifier) int64 {
	var out int64
	for _, m := range mods {
		if m&input.ModAlt != 0 {
			out |= 1
		}
		if m&input.ModCtrl != 0 {
			out |= 2
		}
		if m&input.ModWin != 0 {
			out |= 4
		}
		if m&input.ModShift != 0 {
			out |= 8
		}
	}
	return out
}

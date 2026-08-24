package input

import (
	"context"
	"math/rand"
	"strings"
	"time"
)

// Keyboard adds human-like typing on top of Backend's per-rune key dispatch.
type Keyboard struct {
	backend Backend
	// MinDelay/MaxDelay set the per-character delay range. Defaults: 50–150ms.
	MinDelay, MaxDelay time.Duration
}

func NewKeyboard(b Backend) *Keyboard {
	return &Keyboard{
		backend:  b,
		MinDelay: 50 * time.Millisecond,
		MaxDelay: 150 * time.Millisecond,
	}
}

// Type sends each rune in text via the backend, sleeping a random delay
// between key presses for a human-ish cadence.
func (k *Keyboard) Type(ctx context.Context, text string) error {
	for _, ch := range normalizeLineBreaks(text) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := k.emitRune(ctx, ch); err != nil {
			return err
		}
		k.sleep()
	}
	return nil
}

// normalizeLineBreaks folds CRLF and a lone CR into LF, so one line break is
// one rune — and therefore one Enter press — whatever convention the caller's
// text arrived in. Text submitted from an HTML textarea carries CRLF.
func normalizeLineBreaks(text string) string {
	if !strings.ContainsRune(text, '\r') {
		return text
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

// emitRune dispatches one rune of typed text. A line break is not a printable
// character, and routing it through KeyChar gets it wrong on both backends.
//
// The CDP backend encodes a line break as a keyDown plus a SEPARATE char
// event. An editor that handles Enter in its own keydown handler — Draft.js,
// which powers the X composer — acts on the keydown and then still receives
// the char it already consumed, so one break in the source text lands as two
// on the page. The native backend injects U+000A as a Unicode scan code,
// which Chromium drops on the floor, losing the break entirely.
//
// Both backends agree on VK_RETURN, so a line break goes out as the key a
// human would actually press.
func (k *Keyboard) emitRune(ctx context.Context, ch rune) error {
	if ch == '\n' || ch == '\r' {
		return k.backend.KeyCombo(ctx, VKReturn, 0)
	}
	return k.backend.KeyChar(ctx, ch)
}

// TypeFast skips the random delay loop and asks the backend to bulk-insert.
// The CDP backend pastes the string in one event; the native backend still
// goes per-rune (winapi has no bulk-text primitive).
func (k *Keyboard) TypeFast(ctx context.Context, text string) error {
	return k.backend.InsertText(ctx, text)
}

// Press a single rune (down + up immediately). A line break goes out as an
// Enter press — see emitRune.
func (k *Keyboard) Press(ctx context.Context, ch rune) error {
	return k.emitRune(ctx, ch)
}

// Combo presses a virtual-key code with the given modifiers held. Convenience
// wrapper over Backend.KeyCombo so callers stay on the Keyboard layer.
func (k *Keyboard) Combo(ctx context.Context, vk uint16, mods Modifier) error {
	return k.backend.KeyCombo(ctx, vk, mods)
}

// ClearInput selects all (Ctrl+A) then deletes — the keystroke flow a human
// uses to wipe an input field. Works on both backends because Backend.KeyCombo
// is implemented for each. Caller is responsible for ensuring the target
// field has focus before calling; otherwise the select-all hits whatever the
// browser considers active.
func (k *Keyboard) ClearInput(ctx context.Context) error {
	if err := k.backend.KeyCombo(ctx, VKA, ModCtrl); err != nil {
		return err
	}
	return k.backend.KeyCombo(ctx, VKDelete, 0)
}

func (k *Keyboard) sleep() {
	min := int(k.MinDelay / time.Millisecond)
	max := int(k.MaxDelay / time.Millisecond)
	if max <= min {
		time.Sleep(k.MinDelay)
		return
	}
	time.Sleep(time.Duration(min+rand.Intn(max-min)) * time.Millisecond)
}

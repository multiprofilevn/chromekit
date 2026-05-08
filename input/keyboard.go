package input

import (
	"context"
	"math/rand"
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
	for _, ch := range text {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := k.backend.KeyChar(ctx, ch); err != nil {
			return err
		}
		k.sleep()
	}
	return nil
}

// TypeFast skips the random delay loop and asks the backend to bulk-insert.
// The CDP backend pastes the string in one event; the native backend still
// goes per-rune (winapi has no bulk-text primitive).
func (k *Keyboard) TypeFast(ctx context.Context, text string) error {
	return k.backend.InsertText(ctx, text)
}

// Press a single rune (down + up immediately).
func (k *Keyboard) Press(ctx context.Context, ch rune) error {
	return k.backend.KeyChar(ctx, ch)
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

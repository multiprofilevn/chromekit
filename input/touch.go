package input

import (
	"context"
	"math/rand"
	"time"
)

// TouchScreen wraps Backend's mouse primitives to mimic touch interactions.
// CDP backend treats this as MouseLeft events on the touch emulation layer;
// callers can flip Chrome into mobile emulation mode separately.
type TouchScreen struct {
	backend Backend
}

func NewTouchScreen(b Backend) *TouchScreen { return &TouchScreen{backend: b} }

// Tap presses and releases at (x, y) with a short random hold.
func (t *TouchScreen) Tap(ctx context.Context, x, y float64) error {
	if err := t.backend.MouseMove(ctx, x, y); err != nil {
		return err
	}
	if err := t.backend.MouseDown(ctx, x, y, MouseLeft); err != nil {
		return err
	}
	time.Sleep(time.Duration(60+rand.Intn(40)) * time.Millisecond)
	return t.backend.MouseUp(ctx, x, y, MouseLeft)
}

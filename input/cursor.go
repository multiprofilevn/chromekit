package input

import (
	"context"
	"math/rand"
	"time"
)

// Cursor adds a human-like layer over Backend's mouse primitives.
// Tracks position so MoveTo can interpolate from where it left off.
type Cursor struct {
	backend Backend
	x, y    float64
}

func NewCursor(b Backend) *Cursor { return &Cursor{backend: b} }

// Position returns the last x, y the cursor was moved to.
func (c *Cursor) Position() (float64, float64) { return c.x, c.y }

// MoveTo glides from the current position to (x, y) over a few intermediate
// steps with small jitter, sleeping a few ms between each.
func (c *Cursor) MoveTo(ctx context.Context, x, y float64) error {
	steps := stepCount(c.x, c.y, x, y)
	for _, p := range interpolate(c.x, c.y, x, y, steps) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.backend.MouseMove(ctx, p.X, p.Y); err != nil {
			return err
		}
		time.Sleep(time.Duration(5+rand.Intn(15)) * time.Millisecond)
	}
	c.x, c.y = x, y
	return nil
}

// Click moves to (x, y) and performs a press/release with a random hold time.
func (c *Cursor) Click(ctx context.Context, x, y float64, b MouseButton) error {
	if err := c.MoveTo(ctx, x, y); err != nil {
		return err
	}
	if err := c.backend.MouseDown(ctx, x, y, b); err != nil {
		return err
	}
	time.Sleep(time.Duration(80+rand.Intn(50)) * time.Millisecond)
	return c.backend.MouseUp(ctx, x, y, b)
}

// Scroll dispatches a wheel event.
func (c *Cursor) Scroll(ctx context.Context, deltaX, deltaY float64) error {
	return c.backend.MouseScroll(ctx, deltaX, deltaY)
}

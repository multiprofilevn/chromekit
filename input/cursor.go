package input

import (
	"context"
	"math/rand"
	"time"
)

// Profile selects how Cursor.MoveTo paints the path between two points.
// Mirrors chromekit.CursorProfile so the input package stays self-contained.
type Profile int

const (
	// ProfileHuman draws a Bezier curve with ease-in-out and per-distance step
	// count. Default.
	ProfileHuman Profile = iota
	// ProfileFast uses linear interpolation with minimal sleep.
	ProfileFast
	// ProfileMachine moves in one shot — no interpolation.
	ProfileMachine
)

// Cursor adds a human-like layer over Backend's mouse primitives.
// Tracks position so MoveTo can interpolate from where it left off.
type Cursor struct {
	backend Backend
	profile Profile
	x, y    float64
}

// NewCursor builds a Cursor with the default human profile.
func NewCursor(b Backend) *Cursor { return &Cursor{backend: b, profile: ProfileHuman} }

// NewCursorWithProfile builds a Cursor with an explicit movement profile.
func NewCursorWithProfile(b Backend, p Profile) *Cursor {
	return &Cursor{backend: b, profile: p}
}

// Position returns the last x, y the cursor was moved to.
func (c *Cursor) Position() (float64, float64) { return c.x, c.y }

// cursorSeeder is implemented by backends that have a real OS cursor — they
// can report its current position in the backend's CSS coordinate space.
// Lets MoveTo start the path from where the cursor physically is, not from
// the last CSS coord this Cursor instance happened to set. Critical when
// multiple Cursor instances share one OS pointer (e.g. one Cursor per browser
// in chromefleet) — without this they all teleport between browsers.
type cursorSeeder interface {
	OSPositionCSS() (float64, float64, bool)
}

// MoveTo glides from the current position to (x, y). Behavior depends on the
// configured profile (ProfileHuman by default).
func (c *Cursor) MoveTo(ctx context.Context, x, y float64) error {
	if seeder, ok := c.backend.(cursorSeeder); ok {
		if sx, sy, ok := seeder.OSPositionCSS(); ok {
			c.x, c.y = sx, sy
		}
	}
	defer func() { c.x, c.y = x, y }()

	switch c.profile {
	case ProfileMachine:
		if err := ctx.Err(); err != nil {
			return err
		}
		return c.backend.MouseMove(ctx, x, y)

	case ProfileFast:
		steps := stepCount(c.x, c.y, x, y)
		for _, p := range interpolate(c.x, c.y, x, y, steps) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := c.backend.MouseMove(ctx, p.X, p.Y); err != nil {
				return err
			}
			time.Sleep(time.Duration(2+rand.Intn(4)) * time.Millisecond)
		}
		return nil

	default: // ProfileHuman
		steps := humanStepCount(c.x, c.y, x, y)
		for _, p := range bezierPath(c.x, c.y, x, y, steps) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := c.backend.MouseMove(ctx, p.X, p.Y); err != nil {
				return err
			}
			time.Sleep(time.Duration(8+rand.Intn(11)) * time.Millisecond)
		}
		return nil
	}
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

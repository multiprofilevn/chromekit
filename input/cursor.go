package input

import (
	"context"
	"math/rand"
	"net/http"
	"time"
)

// remotePathTimeout bounds a single mouse-positions call so a slow/dead API
// fails fast to the local bezier fallback instead of stalling every move.
const remotePathTimeout = 1200 * time.Millisecond

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
	// mousePathURL, when non-empty, is the full URL of a mouse-positions
	// endpoint that returns a high-entropy {x,y,delay} path. ProfileHuman
	// fetches from it and replays; any failure falls back to the local bezier.
	mousePathURL string
	httpc        *http.Client
}

// NewCursor builds a Cursor with the default human profile.
func NewCursor(b Backend) *Cursor { return &Cursor{backend: b, profile: ProfileHuman} }

// NewCursorWithProfile builds a Cursor with an explicit movement profile.
func NewCursorWithProfile(b Backend, p Profile) *Cursor {
	return &Cursor{backend: b, profile: p}
}

// NewCursorWithProfileAndPath builds a Cursor that, on ProfileHuman, routes
// moves through a remote mouse-positions endpoint (mousePathURL). Empty URL
// behaves exactly like NewCursorWithProfile (pure local bezier).
func NewCursorWithProfileAndPath(b Backend, p Profile, mousePathURL string) *Cursor {
	c := &Cursor{backend: b, profile: p, mousePathURL: mousePathURL}
	if mousePathURL != "" {
		c.httpc = &http.Client{Timeout: remotePathTimeout}
	}
	return c
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
		// Prefer the remote high-entropy path when configured; fall back to the
		// local bezier on any error / off-target result so moves never break.
		if c.mousePathURL != "" {
			if pts, err := fetchRemotePath(ctx, c.httpc, c.mousePathURL, c.x, c.y, x, y); err == nil && validRemotePath(pts, x, y) {
				for _, p := range pts {
					if err := ctx.Err(); err != nil {
						return err
					}
					if err := c.backend.MouseMove(ctx, p.X, p.Y); err != nil {
						return err
					}
					if p.Delay > 0 {
						time.Sleep(time.Duration(p.Delay) * time.Millisecond)
					}
				}
				return nil
			}
		}
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

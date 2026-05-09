package input

import (
	"context"
	"testing"
)

// fakeBackend records every MouseMove call. Doubles as a cursorSeeder
// reporting a configurable OS-CSS position so we can assert MoveTo seeds
// the start point from the seeder, not the Cursor's cached (c.x, c.y).
type fakeBackend struct {
	moves    []Point
	osCSSX   float64
	osCSSY   float64
	hasOSPos bool
}

func (f *fakeBackend) MouseMove(_ context.Context, x, y float64) error {
	f.moves = append(f.moves, Point{X: x, Y: y})
	return nil
}
func (f *fakeBackend) MouseDown(_ context.Context, _, _ float64, _ MouseButton) error { return nil }
func (f *fakeBackend) MouseUp(_ context.Context, _, _ float64, _ MouseButton) error   { return nil }
func (f *fakeBackend) MouseScroll(_ context.Context, _, _ float64) error              { return nil }
func (f *fakeBackend) KeyChar(_ context.Context, _ rune) error                        { return nil }
func (f *fakeBackend) InsertText(_ context.Context, _ string) error                   { return nil }

func (f *fakeBackend) OSPositionCSS() (float64, float64, bool) {
	return f.osCSSX, f.osCSSY, f.hasOSPos
}

func TestMoveTo_SeedsStartFromOSPosition(t *testing.T) {
	// Cursor's cached position says (0,0), but the OS reports (-200, 50)
	// in this backend's CSS — meaning the real cursor sits 200px to the
	// left of this browser's origin (e.g. cursor is on a different
	// browser). MoveTo to (100, 100) should produce a path that starts
	// near (-200, 50), not near (0, 0).
	fb := &fakeBackend{osCSSX: -200, osCSSY: 50, hasOSPos: true}
	c := NewCursorWithProfile(fb, ProfileFast) // Fast = linear, easier to assert
	if err := c.MoveTo(context.Background(), 100, 100); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if len(fb.moves) == 0 {
		t.Fatal("expected MouseMove calls, got 0")
	}
	first := fb.moves[0]
	// Linear interp from (-200,50) → (100,100) over N steps. The first
	// step should be much closer to (-200,50) than to (0,0).
	if first.X > -100 {
		t.Errorf("first move X=%v should reflect OS start near -200, not cached 0", first.X)
	}
}

func TestMoveTo_NoSeederFallsBackToCached(t *testing.T) {
	// Backend without OSPositionCSS — Cursor falls back to its cached
	// (c.x, c.y). After moving to (50, 50), the next move should start
	// from (50, 50).
	fb := &noSeederBackend{}
	c := NewCursorWithProfile(fb, ProfileFast)
	_ = c.MoveTo(context.Background(), 50, 50)
	fb.moves = nil
	_ = c.MoveTo(context.Background(), 200, 200)
	first := fb.moves[0]
	if first.X < 50 || first.X > 100 {
		t.Errorf("without seeder, first step X=%v should reflect cached start near 50", first.X)
	}
}

// noSeederBackend deliberately does NOT implement cursorSeeder.
type noSeederBackend struct{ moves []Point }

func (b *noSeederBackend) MouseMove(_ context.Context, x, y float64) error {
	b.moves = append(b.moves, Point{X: x, Y: y})
	return nil
}
func (b *noSeederBackend) MouseDown(_ context.Context, _, _ float64, _ MouseButton) error {
	return nil
}
func (b *noSeederBackend) MouseUp(_ context.Context, _, _ float64, _ MouseButton) error { return nil }
func (b *noSeederBackend) MouseScroll(_ context.Context, _, _ float64) error            { return nil }
func (b *noSeederBackend) KeyChar(_ context.Context, _ rune) error                      { return nil }
func (b *noSeederBackend) InsertText(_ context.Context, _ string) error                 { return nil }

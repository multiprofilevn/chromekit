package input

import (
	"context"
	"sync"
	"testing"
)

// A Cursor tracks its own position so MoveTo can interpolate from where it
// left off. Two goroutines moving the same page's cursor therefore race on
// x/y AND interleave their paths, producing a cursor that jumps between two
// destinations mid-gesture.

// moveRecorder serializes nothing itself — any interleaving observed in a
// test comes from Cursor, which is exactly what is under test.
type moveRecorder struct {
	mu    sync.Mutex
	moves []Point
}

func (b *moveRecorder) MouseMove(_ context.Context, x, y float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.moves = append(b.moves, Point{X: x, Y: y})
	return nil
}
func (b *moveRecorder) MouseDown(_ context.Context, _, _ float64, _ MouseButton) error {
	return nil
}
func (b *moveRecorder) MouseUp(_ context.Context, _, _ float64, _ MouseButton) error { return nil }
func (b *moveRecorder) MouseScroll(_ context.Context, _, _ float64) error            { return nil }
func (b *moveRecorder) KeyChar(_ context.Context, _ rune) error                      { return nil }
func (b *moveRecorder) InsertText(_ context.Context, _ string) error                 { return nil }
func (b *moveRecorder) KeyCombo(_ context.Context, _ uint16, _ Modifier) error       { return nil }

func (b *moveRecorder) snapshot() []Point {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Point, len(b.moves))
	copy(out, b.moves)
	return out
}

func TestCursor_ConcurrentMoveIsSerialized(t *testing.T) {
	be := &moveRecorder{}
	c := NewCursorWithProfile(be, ProfileFast)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Distinct destinations so an interleave would be visible as a
			// path that never settles.
			if err := c.MoveTo(context.Background(), float64(100+i*40), float64(100+i*40)); err != nil {
				t.Errorf("MoveTo: %v", err)
			}
		}(i)
	}
	wg.Wait()

	moves := be.snapshot()
	if len(moves) == 0 {
		t.Fatal("no moves recorded")
	}
	// Each gesture ends exactly on its destination. If gestures interleaved,
	// the final point would be a midpoint of some other goroutine's path.
	last := moves[len(moves)-1]
	onADestination := false
	for i := 0; i < 8; i++ {
		if last.X == float64(100+i*40) && last.Y == float64(100+i*40) {
			onADestination = true
			break
		}
	}
	if !onADestination {
		t.Fatalf("last move %v is not any goroutine's destination — gestures interleaved", last)
	}
}

func TestCursor_ConcurrentClickAndScroll(t *testing.T) {
	// Exercises the other two entry points under -race; the assertion is the
	// race detector plus "does not deadlock".
	be := &moveRecorder{}
	c := NewCursorWithProfile(be, ProfileFast)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); _ = c.MoveTo(context.Background(), 10, 10) }()
		go func() { defer wg.Done(); _ = c.Click(context.Background(), 20, 20, MouseLeft) }()
		go func() { defer wg.Done(); _ = c.Scroll(context.Background(), 0, 5) }()
	}
	wg.Wait()
}

// SetWindow is unused by this fake — the frame lives in the real native backend.
func (*moveRecorder) SetWindow(Window) {}

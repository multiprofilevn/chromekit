package input

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingBackend records how many MouseMove calls are in flight at once so a
// test can assert the gesture limiter caps concurrency.
type countingBackend struct {
	inflight int32
	maxSeen  int32
	hold     time.Duration
}

func (b *countingBackend) MouseMove(ctx context.Context, x, y float64) error {
	n := atomic.AddInt32(&b.inflight, 1)
	for {
		old := atomic.LoadInt32(&b.maxSeen)
		if n <= old || atomic.CompareAndSwapInt32(&b.maxSeen, old, n) {
			break
		}
	}
	if b.hold > 0 {
		time.Sleep(b.hold)
	}
	atomic.AddInt32(&b.inflight, -1)
	return nil
}

func (b *countingBackend) MouseDown(context.Context, float64, float64, MouseButton) error {
	return nil
}
func (b *countingBackend) MouseUp(context.Context, float64, float64, MouseButton) error { return nil }
func (b *countingBackend) MouseScroll(context.Context, float64, float64) error          { return nil }
func (b *countingBackend) KeyChar(context.Context, rune) error                          { return nil }
func (b *countingBackend) InsertText(context.Context, string) error                     { return nil }
func (b *countingBackend) KeyCombo(context.Context, uint16, Modifier) error             { return nil }

// restoreLimit snapshots and restores the process-global limit around a test so
// tests don't leak state into one another.
func restoreLimit(t *testing.T) {
	t.Helper()
	gestureMu.Lock()
	prev := gestureLimit
	gestureMu.Unlock()
	t.Cleanup(func() { SetGestureLimit(prev) })
}

func TestGestureLimiter_CapsConcurrency(t *testing.T) {
	restoreLimit(t)
	SetGestureLimit(2)

	be := &countingBackend{hold: 20 * time.Millisecond}
	// One cursor per goroutine (a real page has one), all sharing the same
	// process-global limiter — that is what we are exercising.
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := NewCursorWithProfile(be, ProfileFast)
			_ = c.MoveTo(context.Background(), 500, 500)
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&be.maxSeen); got > 2 {
		t.Fatalf("max concurrent MouseMove = %d, want <= 2", got)
	}
}

func TestGestureLimiter_Unlimited(t *testing.T) {
	restoreLimit(t)
	SetGestureLimit(0)

	if sem := currentSem(); sem != nil {
		t.Fatalf("SetGestureLimit(0) should leave a nil semaphore, got %v", sem)
	}
	be := &countingBackend{}
	c := NewCursorWithProfile(be, ProfileFast)
	if err := c.MoveTo(context.Background(), 10, 10); err != nil {
		t.Fatalf("MoveTo under unlimited: %v", err)
	}
}

func TestGestureLimiter_CancelWhileQueued(t *testing.T) {
	restoreLimit(t)
	SetGestureLimit(1)

	// Fill the single slot and hold it.
	held, err := acquireGesture(context.Background())
	if err != nil {
		t.Fatalf("prime acquire: %v", err)
	}
	defer held()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		release, err := acquireGesture(ctx)
		release()
		done <- err
	}()

	// Let the goroutine reach the blocking select, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("acquire while queued should return ctx error after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("acquire did not unblock on cancel")
	}
}

func TestGestureLimiter_ReentrantSingleSlot(t *testing.T) {
	restoreLimit(t)
	SetGestureLimit(1)

	// Hold the one slot via AcquireGesture, then nested MoveTo+Click under the
	// returned ctx must NOT deadlock (they inherit the slot).
	ctx, release, err := AcquireGesture(context.Background())
	if err != nil {
		t.Fatalf("AcquireGesture: %v", err)
	}
	defer release()

	be := &countingBackend{}
	c := NewCursorWithProfile(be, ProfileFast)

	done := make(chan error, 1)
	go func() {
		if err := c.MoveTo(ctx, 100, 100); err != nil {
			done <- err
			return
		}
		done <- c.Click(ctx, 100, 100, MouseLeft)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("nested gesture under held slot: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("nested gesture deadlocked — reentrancy token not honored")
	}
}

func TestGestureLimiter_SwapMidFlightNoDeadlock(t *testing.T) {
	restoreLimit(t)
	SetGestureLimit(2)

	be := &countingBackend{hold: 5 * time.Millisecond}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := NewCursorWithProfile(be, ProfileFast)
			_ = c.MoveTo(context.Background(), 300, 300)
		}()
	}
	// Swap the channel while gestures are in flight — release must drain into
	// the channel it acquired from, never panic or deadlock.
	for _, n := range []int{1, 4, 2, 0, 3} {
		SetGestureLimit(n)
		time.Sleep(2 * time.Millisecond)
	}
	wg.Wait()
}

// SetWindow is unused by this fake — the frame lives in the real native backend.
func (*countingBackend) SetWindow(Window) {}

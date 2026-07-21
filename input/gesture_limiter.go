package input

import (
	"context"
	"sync"
)

// DefaultGestureLimit caps how many mouse gestures run concurrently in one
// process. A gesture is dozens of CDP mouse-move dispatches, each of which
// blocks on a renderer acknowledgement; when 200 tabs render at once the CPU
// saturates and those acks balloon from sub-millisecond to hundreds of
// milliseconds, dragging every gesture into multi-second territory. Capping the
// number of gestures in flight keeps the active renderer set small enough that
// acks stay fast — fewer things moving at once finish sooner than everything
// crawling together.
//
// On a 40-thread host shared by 4 runner processes, 10 per process (40 total)
// tracks the thread count. Tune per (CPU threads / process count). 0 disables
// the limiter.
const DefaultGestureLimit = 10

var (
	gestureMu    sync.Mutex
	gestureSem   chan struct{} // buffered N; nil means unlimited
	gestureLimit = DefaultGestureLimit
)

func init() {
	gestureSem = make(chan struct{}, DefaultGestureLimit)
}

// gestureHeldKey marks a context whose caller already holds a gesture slot, so
// a nested acquire (a compound gesture that calls MoveTo/Click internally) does
// not try to take a second slot — which would self-deadlock at limit 1 and, at
// higher limits, let one logical gesture occupy several slots.
type gestureHeldKey struct{}

// SetGestureLimit changes the per-process concurrent-gesture cap. n <= 0 means
// unlimited. Safe to call at any time: a gesture already holding a slot releases
// into whichever channel it acquired from, so a mid-flight swap never loses or
// double-counts a slot. Typical use is once at startup.
//
// The limiter is per-process by design. With one runner process per OS user
// (e.g. 4 RDP users on one box) each process caps itself independently, which
// is why this is a package-level function and not a per-Browser option — two
// Browsers in the same process share the one cap instead of silently
// overwriting each other's setting.
func SetGestureLimit(n int) {
	gestureMu.Lock()
	defer gestureMu.Unlock()
	gestureLimit = n
	if n <= 0 {
		gestureSem = nil
		return
	}
	gestureSem = make(chan struct{}, n)
}

// currentSem returns the active semaphore channel (nil = unlimited).
func currentSem() chan struct{} {
	gestureMu.Lock()
	defer gestureMu.Unlock()
	return gestureSem
}

// acquireGesture blocks until a gesture slot is free or ctx is done. The
// returned release is idempotent and always safe to call — it is a no-op when
// the slot was inherited (reentrant) or the limiter is off. If ctx already
// holds a slot (see AcquireGesture) it returns immediately without taking a
// second one.
func acquireGesture(ctx context.Context) (func(), error) {
	if ctx.Value(gestureHeldKey{}) != nil {
		return func() {}, nil // reentrant: caller already holds the slot
	}
	sem := currentSem()
	if sem == nil {
		return func() {}, nil // unlimited
	}
	select {
	case sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-sem }) }, nil
	case <-ctx.Done():
		return func() {}, ctx.Err()
	}
}

// AcquireGesture takes one gesture slot for a compound gesture (move + press +
// release, or a drag loop) and returns a child context carrying a "slot held"
// marker. Every acquireGesture nested under the returned context is a no-op, so
// the whole compound gesture holds exactly one slot for its full duration and
// its sub-steps never queue behind the global limit and split apart under load
// (e.g. a double-click whose second press waits in the queue long enough to
// register as two separate single clicks).
//
// Caller MUST use the returned context for the gesture's sub-steps and call the
// returned release exactly once when the gesture ends.
func AcquireGesture(ctx context.Context) (context.Context, func(), error) {
	release, err := acquireGesture(ctx)
	if err != nil {
		return ctx, release, err
	}
	return context.WithValue(ctx, gestureHeldKey{}, struct{}{}), release, nil
}

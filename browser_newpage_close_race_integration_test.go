//go:build integration

package chromekit

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestIntegration_NewPageCloseRace hammers NewPage concurrently with Close on a
// real headless Chrome. It guards the TOCTOU fix in NewPage: a create that
// races a Close must either succeed fully or abort cleanly (pageCancel +
// orphan-target close), never append to a nil'd b.pages or strand a tab. Run
// under -race; the detector is the primary assertion, plus: no panic, and every
// post-close NewPage returns an error rather than a live page.
func TestIntegration_NewPageCloseRace(t *testing.T) {
	chromePath := resolveChromePath(t)

	for iter := 0; iter < 5; iter++ {
		b, err := Launch(chromePath, WithHeadless(true))
		if err != nil {
			t.Fatalf("launch: %v", err)
		}

		var wg sync.WaitGroup

		// Several openers racing the close.
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 15; i++ {
					_, err := b.NewPage()
					if err != nil && !errors.Is(err, ErrContextCanceled) && !errors.Is(err, ErrNavigate) {
						t.Errorf("unexpected NewPage error: %v", err)
					}
				}
			}()
		}

		// Let a few pages open, then close mid-flight.
		time.Sleep(10 * time.Millisecond)
		b.Close()
		wg.Wait()

		// Invariant: Close() nils b.pages under the lock; a NewPage that lost the
		// race must have aborted (re-check under lock) rather than appended after
		// the nil. If b.pages is non-empty here, a late append revived it — the
		// exact TOCTOU this phase fixes.
		b.pagesMu.RLock()
		revived := len(b.pages)
		b.pagesMu.RUnlock()
		if revived != 0 {
			t.Fatalf("iter %d: b.pages revived with %d entries after Close (TOCTOU leak)", iter, revived)
		}
	}
}

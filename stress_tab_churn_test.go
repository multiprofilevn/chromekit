//go:build stress

package chromekit

import (
	"math/rand"
	"sync"
	"testing"
	"time"
)

// Tab churn under gesture load: opening, navigating, switching, and closing
// pages while gestures are in flight on another page must never panic,
// deadlock, or strand goroutines — page-map mutation races per-page op
// lifecycles by design here. Runs on both the Launch path and the Connect
// path, because browser-scope call routing differs between the two.

// TestStress_TabChurnUnderGestureLoad drives, per browser: (A) a continuous
// gesture loop on a fixed page, (B) an open→navigate→close tab churn loop,
// (C) a reader loop hitting Pages/Current/SwitchPageByIndex mid-mutation.
//
// Run: STRESS_BROWSERS=5 STRESS_DURATION_SEC=30 \
//      go test -tags stress -run TestStress_TabChurnUnderGestureLoad -v .
func TestStress_TabChurnUnderGestureLoad(t *testing.T) {
	chromePath := resolveChromePath(t)
	nBrowsers := envInt("STRESS_BROWSERS", 5)
	dur := time.Duration(envInt("STRESS_DURATION_SEC", 30)) * time.Second
	basePort := envInt("STRESS_DEBUG_PORT_BASE", 9800)

	t.Run("launch", func(t *testing.T) {
		baseline := stableGoroutineCount()
		browsers := make([]*Browser, 0, nBrowsers)
		for i := 0; i < nBrowsers; i++ {
			b, err := Launch(chromePath, WithHeadless(true))
			if err != nil {
				t.Fatalf("browser %d launch: %v", i, err)
			}
			browsers = append(browsers, b)
		}
		runTabChurn(t, baseline, browsers, dur)
	})

	t.Run("connect", func(t *testing.T) {
		baseline := stableGoroutineCount()
		browsers := make([]*Browser, 0, nBrowsers)
		for i := 0; i < nBrowsers; i++ {
			port := basePort + i
			startDebugChrome(t, port)
			b, err := Connect(port)
			if err != nil {
				t.Fatalf("browser %d connect port %d: %v", i, port, err)
			}
			browsers = append(browsers, b)
		}
		runTabChurn(t, baseline, browsers, dur)
	})
}

func runTabChurn(t *testing.T, baseline int, browsers []*Browser, dur time.Duration) {
	t.Helper()
	rec := newLatencyRecorder()
	deadline := time.Now().Add(dur)
	var wg sync.WaitGroup

	// Per browser: the fixed gesture page plus whatever pages pre-existed
	// (Connect attaches to the guest browser's own tabs). Everything beyond
	// this set is churn debris to sweep before the settle assertion.
	startIDs := make([]map[string]bool, len(browsers))

	for bi, b := range browsers {
		p, err := b.NewPage()
		if err != nil {
			t.Fatalf("browser %d fixed page: %v", bi, err)
		}
		if err := p.Navigate(stressPage, 15*time.Second); err != nil {
			t.Logf("browser %d fixed-page navigate: %v (continuing)", bi, err)
		}
		ids := map[string]bool{}
		for _, ep := range b.Pages() {
			ids[ep.ID()] = true
		}
		startIDs[bi] = ids

		wg.Add(3)

		// A: continuous gestures on the fixed page. Gesture errors while a
		// sibling tab is being torn down are tolerated; only panics/deadlocks
		// and leaks fail the test.
		go func(p *Page, seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for time.Now().Before(deadline) {
				x := 10 + rng.Float64()*300
				y := 10 + rng.Float64()*200
				start := time.Now()
				err := p.Mouse().MoveTo(x, y)
				rec.record("gesture", time.Since(start), err)
				start = time.Now()
				err = p.Mouse().ClickAt(x, y)
				rec.record("gesture", time.Since(start), err)
			}
		}(p, int64(bi))

		// B: open → navigate → close churn.
		go func(b *Browser) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				start := time.Now()
				np, err := b.NewPage()
				if err != nil {
					rec.record("churn", time.Since(start), err)
					time.Sleep(50 * time.Millisecond)
					continue
				}
				if err := np.Navigate("about:blank", 10*time.Second); err != nil {
					rec.record("churn-nav", time.Since(start), err)
				}
				err = b.ClosePage(np.ID())
				rec.record("churn", time.Since(start), err)
			}
		}(b)

		// C: reads racing the mutation above. SwitchPageByIndex may land on a
		// page mid-close or switch to an unexpected page — errors tolerated.
		go func(b *Browser, seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for time.Now().Before(deadline) {
				_ = b.Pages()
				_ = b.Current()
				_ = b.SwitchPageByIndex(rng.Intn(3))
				time.Sleep(20 * time.Millisecond)
			}
		}(b, int64(bi)+1000)
	}

	wg.Wait()

	// The churn loop closes what it opens, but its last iteration can lose the
	// race with the deadline — sweep leftovers, then the tracked page count
	// must return exactly to the pre-churn set (proves the page map was not
	// corrupted by concurrent mutation).
	for bi, b := range browsers {
		for _, ep := range b.Pages() {
			if !startIDs[bi][ep.ID()] {
				if err := b.ClosePage(ep.ID()); err != nil {
					t.Logf("browser %d sweep close %s: %v", bi, ep.ID(), err)
				}
			}
		}
		awaitPageCount(t, b, len(startIDs[bi]), 10*time.Second)
	}

	for _, b := range browsers {
		b.Close()
	}

	t.Log("=== tab churn results ===")
	for _, s := range rec.stats() {
		t.Logf("%-10s n=%-7d err=%-5d p50=%-10v p95=%-10v max=%v",
			s.op, s.n, s.errs, s.p50.Round(time.Millisecond),
			s.p95.Round(time.Millisecond), s.max.Round(time.Millisecond))
	}

	if !awaitGoroutinesAtMost(baseline+8, 15*time.Second) {
		t.Errorf("goroutines did not settle: baseline=%d now=%d", baseline, stableGoroutineCount())
	}
}

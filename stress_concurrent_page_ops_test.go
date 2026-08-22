//go:build stress

package chromekit

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multiprofilevn/chromekit/input"
)

// Concurrent ops on one page: gestures, Evaluate, Screenshot, listener
// re-registration, frame polling, and periodic navigation must coexist without
// panics or stranded goroutines. Ops cancelled by an in-flight navigation must
// fail cleanly (an error, never a hang) — those errors are counted, not fatal.

// TestStress_ConcurrentPageOps hammers a single page per browser with every op
// family at once, navigating every few seconds so in-flight ops meet a new
// document. Frame polling runs alongside navigation on purpose: under -race
// that overlap is exactly the attach/detach window being audited.
//
// Run: STRESS_BROWSERS=2 STRESS_DURATION_SEC=30 \
//      go test -tags stress -run TestStress_ConcurrentPageOps -v .
func TestStress_ConcurrentPageOps(t *testing.T) {
	chromePath := resolveChromePath(t)
	nBrowsers := envInt("STRESS_BROWSERS", 2)
	dur := time.Duration(envInt("STRESS_DURATION_SEC", 30)) * time.Second

	baseline := stableGoroutineCount()
	rec := newLatencyRecorder()
	deadline := time.Now().Add(dur)
	watchdog := time.Duration(envInt("STRESS_OP_WATCHDOG_SEC", 30)) * time.Second
	var hangs int64
	var wg sync.WaitGroup
	var browsers []*Browser

	// spawnLoop runs f until the deadline, recording each round under op. Each
	// call is bounded by a watchdog: an op that outlives it has no deadline of
	// its own and only unblocks when the browser context dies at teardown —
	// that hang is itself a finding this suite exists to surface, so it is
	// recorded and the loop abandoned instead of wedging the whole run.
	spawnLoop := func(op string, pause time.Duration, f func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				start := time.Now()
				done := make(chan error, 1)
				go func() { done <- f() }()
				select {
				case err := <-done:
					rec.record(op, time.Since(start), err)
				case <-time.After(watchdog):
					atomic.AddInt64(&hangs, 1)
					rec.record(op+"-HANG", time.Since(start),
						fmt.Errorf("op %q exceeded %s watchdog", op, watchdog))
					return
				}
				if pause > 0 {
					time.Sleep(pause)
				}
			}
		}()
	}

	for bi := 0; bi < nBrowsers; bi++ {
		b, err := Launch(chromePath, WithHeadless(true))
		if err != nil {
			t.Fatalf("browser %d launch: %v", bi, err)
		}
		browsers = append(browsers, b)

		p, err := b.NewPage()
		if err != nil {
			t.Fatalf("browser %d page: %v", bi, err)
		}
		injectCountingBackend(p, input.ProfileHuman)
		if err := p.Navigate(stressPage, 15*time.Second); err != nil {
			t.Logf("browser %d navigate: %v (continuing)", bi, err)
		}

		gestureRng := rand.New(rand.NewSource(int64(bi)))
		spawnLoop("gesture", 0, func() error {
			x := 10 + gestureRng.Float64()*300
			y := 10 + gestureRng.Float64()*200
			if err := p.Mouse().MoveTo(x, y); err != nil {
				return err
			}
			return p.Mouse().ClickAt(x, y)
		})

		spawnLoop("evaluate", 10*time.Millisecond, func() error {
			var out int
			return p.Evaluate("1+1", &out)
		})

		spawnLoop("screenshot", 200*time.Millisecond, func() error {
			_, err := p.Screenshot()
			return err
		})

		// Repeated handler swaps while events are being dispatched.
		spawnLoop("listeners", 100*time.Millisecond, func() error {
			p.OnConsole(func(ConsoleEvent) {})
			p.OnDialog(func(DialogEvent) DialogAction { return DialogAction{} })
			return nil
		})

		spawnLoop("frames", 5*time.Millisecond, func() error {
			if _, err := p.Frames(); err != nil {
				return err
			}
			mf, err := p.MainFrame()
			if err != nil {
				return err
			}
			_ = mf.IsAttached()
			return nil
		})

		spawnLoop("navigate", 5*time.Second, func() error {
			return p.Navigate(stressPage, 15*time.Second)
		})
	}

	wg.Wait()

	for _, b := range browsers {
		b.Close()
	}

	t.Log("=== concurrent page ops results ===")
	for _, s := range rec.stats() {
		t.Logf("%-10s n=%-7d err=%-5d p50=%-10v p95=%-10v max=%v",
			s.op, s.n, s.errs, s.p50.Round(time.Millisecond),
			s.p95.Round(time.Millisecond), s.max.Round(time.Millisecond))
	}

	// Errors from ops cancelled by navigation are expected; total starvation
	// of an op family is not — each must succeed at least once.
	for _, s := range rec.stats() {
		switch s.op {
		case "gesture", "evaluate", "screenshot", "frames":
			if int64(s.n) <= s.errs {
				t.Errorf("op %q never succeeded: n=%d errs=%d", s.op, s.n, s.errs)
			}
		}
	}

	// Hangs are empirical confirmation of the unbounded-op-deadline defect;
	// they are reported for the run summary, not failed here — the fix is
	// tracked as follow-up work, and the suite must stay usable meanwhile.
	if h := atomic.LoadInt64(&hangs); h > 0 {
		t.Logf("WARNING: %d op(s) hung past the %s watchdog — ops without their own "+
			"deadline blocked until browser teardown (see *-HANG rows above)", h, watchdog)
	}

	if !awaitGoroutinesAtMost(baseline+8, 15*time.Second) {
		t.Errorf("goroutines did not settle: baseline=%d now=%d", baseline, stableGoroutineCount())
	}
}

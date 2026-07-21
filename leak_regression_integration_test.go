//go:build integration

package chromekit

import (
	"testing"
	"time"
)

// Baselines these guard, measured before the fixes (50 external closes / 100
// churn cycles against real Chrome):
//
//	tab churn via ClosePage      goroutines flat      (was already clean)
//	tab closed by the page       +3 goroutines/tab    → flat
//	Connect/Close cycle          ~7 goroutines/cycle  → unchanged, see README
//
// The external-close and Connect cases have their own tests next to the code
// that fixes them; this file covers the churn path and owns the shared helper.

func TestLeakRegression_TabChurn(t *testing.T) {
	chromePath := resolveChromePath(t)
	b, err := Launch(chromePath, WithHeadless(true))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	const rounds = 50

	// One warm-up cycle so lazily-created machinery is not counted as growth.
	warm, err := b.NewPage()
	if err != nil {
		t.Fatalf("warmup: %v", err)
	}
	if err := b.ClosePage(warm.ID()); err != nil {
		t.Fatalf("warmup close: %v", err)
	}
	baseline := stableGoroutineCount()

	for i := 0; i < rounds; i++ {
		p, err := b.NewPage()
		if err != nil {
			t.Fatalf("round %d: new page: %v", i, err)
		}
		if err := p.Navigate("data:text/html,<title>churn</title>", 15*time.Second); err != nil {
			t.Fatalf("round %d: navigate: %v", i, err)
		}
		if err := b.ClosePage(p.ID()); err != nil {
			t.Fatalf("round %d: close page: %v", i, err)
		}
	}

	if n := len(b.Pages()); n != 1 {
		t.Fatalf("pages accumulated: %d after %d open/close cycles, want 1", n, rounds)
	}
	if !awaitGoroutinesAtMost(baseline+4, 5*time.Second) {
		t.Fatalf("goroutines grew from %d to %d over %d churn cycles",
			baseline, stableGoroutineCount(), rounds)
	}
}

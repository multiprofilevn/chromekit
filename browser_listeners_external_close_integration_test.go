//go:build integration

package chromekit

import (
	"testing"
	"time"
)

func TestIntegration_ExternalClose_RemovesPage(t *testing.T) {
	chromePath := resolveChromePath(t)
	b, err := Launch(chromePath, WithHeadless(true))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	// Stays on about:blank: Chrome refuses window.close() from a document it
	// did not open by script, so navigating first would test the harness
	// rather than the eviction path.
	p, err := b.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	if len(b.Pages()) != 2 {
		t.Fatalf("expected 2 pages before close, got %d", len(b.Pages()))
	}

	// The page closes itself, exactly like an OAuth popup posting its result.
	_ = p.Evaluate(`window.close()`, nil)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(b.Pages()) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(b.Pages()); n != 1 {
		t.Fatalf("externally closed tab still tracked after 5s: %d pages", n)
	}
	select {
	case <-p.Ctx().Done():
	default:
		t.Error("externally closed tab context still alive — chromedp goroutines leak")
	}
}

func TestIntegration_ExternalClose_GoroutinesStayFlat(t *testing.T) {
	chromePath := resolveChromePath(t)
	b, err := Launch(chromePath, WithHeadless(true))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	const rounds = 20
	// Warm up one cycle so lazily-started machinery is not counted as growth.
	warm, err := b.NewPage()
	if err != nil {
		t.Fatalf("warmup page: %v", err)
	}
	_ = warm.Evaluate(`window.close()`, nil)
	awaitPageCount(t, b, 1, 5*time.Second)

	baseline := stableGoroutineCount()

	for i := 0; i < rounds; i++ {
		p, err := b.NewPage()
		if err != nil {
			t.Fatalf("round %d: new page: %v", i, err)
		}
		_ = p.Evaluate(`window.close()`, nil)
		awaitPageCount(t, b, 1, 5*time.Second)
	}

	if n := len(b.Pages()); n != 1 {
		t.Fatalf("pages accumulated: %d after %d external closes", n, rounds)
	}
	// Pre-fix this leaked ~3 goroutines per closed tab (~60 for 20 rounds).
	if !awaitGoroutinesAtMost(baseline+4, 3*time.Second) {
		t.Fatalf("goroutines grew from %d to %d over %d external closes",
			baseline, stableGoroutineCount(), rounds)
	}
}

// stableGoroutineCount / awaitGoroutinesAtMost / awaitPageCount live in
// stress_helpers_test.go (build tag `integration || stress`) so the stress
// harness can share them.

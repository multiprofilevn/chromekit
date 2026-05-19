package chromekit

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Phase 03 — Phase 02 coverage (B: visibilityState barrier).
// TDD-RED: awaitTabVisible + ErrTabActivateTimeout do not exist yet.
//
// R3 hard contract — every test calls the unexported PURE function
// awaitTabVisible directly with fake activate/probe closures: zero chromedp,
// zero live browser, no package-global hook. Deadline/step are parameters, so
// the determinism gate runs fast (production wires 3s/75ms in BringToFront).
//
// Signature under contract (Phase 02 F6):
//   func awaitTabVisible(ctx context.Context,
//       activate func(context.Context) error,
//       probe func(context.Context) (string, error),
//       deadline, step time.Duration) error

const (
	testDeadline = 100 * time.Millisecond
	testStep     = 10 * time.Millisecond
)

func noActivate(context.Context) error { return nil }

// B-TS1 — success fast: probe 'visible' on first poll → nil, well under deadline.
func TestAwaitTabVisible_VisibleImmediately_ReturnsNilFast(t *testing.T) {
	activateCalls := 0
	activate := func(context.Context) error { activateCalls++; return nil }
	probe := func(context.Context) (string, error) { return "visible", nil }

	start := time.Now()
	err := awaitTabVisible(context.Background(), activate, probe, testDeadline, testStep)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("err = %v, want nil on immediate visible", err)
	}
	if elapsed >= testDeadline {
		t.Errorf("elapsed = %v, want << deadline (no artificial wait on success)", elapsed)
	}
	if activateCalls < 1 {
		t.Errorf("activate called %d times, want >= 1 (must (re)issue activation)", activateCalls)
	}
}

// B-TS2 — THE anti-recurrence gate. Never-visible → deterministic typed
// timeout, NEVER nil, across 50 runs. This is the regression that closes the
// 3x-recurrence root cause: a wrong-tab focus must fail LOUD, never silently
// proceed.
func TestAwaitTabVisible_NeverVisible_DeterministicTimeout(t *testing.T) {
	const iterations = 50
	probe := func(context.Context) (string, error) { return "hidden", nil }

	for i := 0; i < iterations; i++ {
		start := time.Now()
		err := awaitTabVisible(context.Background(), noActivate, probe, testDeadline, testStep)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatalf("iter %d: err = nil — barrier MUST fail loud on never-visible, never silently pass", i)
		}
		if !errors.Is(err, ErrTabActivateTimeout) {
			t.Fatalf("iter %d: err = %v, want errors.Is ErrTabActivateTimeout", i, err)
		}
		if elapsed < testDeadline {
			t.Fatalf("iter %d: elapsed = %v < deadline %v — returned before budget exhausted", i, elapsed, testDeadline)
		}
		// Upper bound kept generous (loaded-CI tolerance); identity asserted
		// strictly, timing loosely (Phase 03 risk mitigation).
		if elapsed > testDeadline+50*testStep {
			t.Fatalf("iter %d: elapsed = %v wildly past deadline %v — loop not bounded", i, elapsed, testDeadline)
		}
	}
}

// B-TS3 — ctx cancel exits promptly, not after the full deadline.
func TestAwaitTabVisible_CtxCancel_ReturnsPromptly(t *testing.T) {
	probe := func(context.Context) (string, error) { return "hidden", nil }
	ctx, cancel := context.WithCancel(context.Background())
	longDeadline := 2 * time.Second

	go func() {
		time.Sleep(3 * testStep)
		cancel()
	}()

	start := time.Now()
	err := awaitTabVisible(ctx, noActivate, probe, longDeadline, testStep)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("err = nil, want ctx-canceled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want errors.Is context.Canceled", err)
	}
	if elapsed >= longDeadline {
		t.Errorf("elapsed = %v, want prompt return on cancel (<< %v)", elapsed, longDeadline)
	}
}

// B-TS5 — sentinel identity survives the %w wrap (callers use errors.Is).
func TestAwaitTabVisible_TimeoutErrorIsWrapped(t *testing.T) {
	probe := func(context.Context) (string, error) { return "hidden", nil }

	err := awaitTabVisible(context.Background(), noActivate, probe, testStep, testStep)

	if !errors.Is(err, ErrTabActivateTimeout) {
		t.Fatalf("err = %v, want errors.Is ErrTabActivateTimeout (%%w wrap)", err)
	}
}

// B-TS-recover — re-issue-activate loop converges: hidden,hidden,visible →
// nil after 3 polls, activate called once per poll (not just first-shot).
func TestAwaitTabVisible_RecoversAfterReissue(t *testing.T) {
	script := []string{"hidden", "hidden", "visible"}
	probeCalls := 0
	probe := func(context.Context) (string, error) {
		v := script[probeCalls]
		probeCalls++
		return v, nil
	}
	activateCalls := 0
	activate := func(context.Context) error { activateCalls++; return nil }

	err := awaitTabVisible(context.Background(), activate, probe, testDeadline, testStep)

	if err != nil {
		t.Fatalf("err = %v, want nil (converges on 3rd poll)", err)
	}
	if probeCalls != 3 {
		t.Errorf("probe called %d times, want exactly 3", probeCalls)
	}
	if activateCalls < 3 {
		t.Errorf("activate called %d times, want >= 3 (re-issued each miss)", activateCalls)
	}
}

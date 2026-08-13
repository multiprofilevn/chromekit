package chromekit

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestOwnedProcessBind_WiresCancelAndFiniteWaitDelay(t *testing.T) {
	owned := newOwnedProcess(func(context.Context, *exec.Cmd, uint64) error { return nil })
	cmd := exec.Command("chrome")

	owned.bind(cmd)

	if cmd.Cancel == nil {
		t.Fatal("owned command has no Cancel function")
	}
	if cmd.WaitDelay <= 0 {
		t.Fatalf("owned command WaitDelay is not finite: %s", cmd.WaitDelay)
	}
}

func TestOwnedProcessTerminate_RetriesFailureAndCachesSuccess(t *testing.T) {
	wantErr := errors.New("terminate failed")
	calls := 0
	owned := newOwnedProcess(func(context.Context, *exec.Cmd, uint64) error {
		calls++
		if calls == 1 {
			return wantErr
		}
		return nil
	})
	owned.bind(exec.Command("chrome"))

	first := owned.Terminate(context.Background())
	second := owned.Terminate(context.Background())

	third := owned.Terminate(context.Background())
	if !errors.Is(first, wantErr) || second != nil || third != nil {
		t.Fatalf("Terminate retry/cache results: first=%v second=%v third=%v", first, second, third)
	}
	if calls != 2 {
		t.Fatalf("terminator called %d times, want one failure + one successful retry", calls)
	}
}

func TestOwnedProcessTerminate_CancelledContextDoesNotPoisonRetry(t *testing.T) {
	calls := 0
	owned := newOwnedProcess(func(context.Context, *exec.Cmd, uint64) error {
		calls++
		return nil
	})
	owned.bind(exec.Command("chrome"))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := owned.Terminate(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Terminate(cancelled) = %v, want context.Canceled", err)
	}
	if err := owned.Terminate(context.Background()); err != nil {
		t.Fatalf("Terminate retry: %v", err)
	}
	if calls != 1 {
		t.Fatalf("terminator called %d times, want 1 successful retry", calls)
	}
}

func TestBrowserClose_LaunchTerminatesBeforeCancellationAndRetriesFailure(t *testing.T) {
	browserCtx, browserCancel := context.WithCancel(context.Background())
	wantErr := errors.New("tree termination failed")
	calls := 0
	owned := newOwnedProcess(func(context.Context, *exec.Cmd, uint64) error {
		calls++
		if calls == 1 && browserCtx.Err() != nil {
			t.Fatal("browser context cancelled before owned process tree termination")
		}
		if calls == 1 {
			return wantErr
		}
		return nil
	})
	owned.bind(exec.Command("chrome"))
	b := &Browser{
		ctx:          browserCtx,
		cancel:       browserCancel,
		cfg:          defaultConfig(),
		log:          noopLogger{},
		ownedProcess: owned,
	}

	first := b.Close()
	second := b.Close()
	third := b.Close()

	if !errors.Is(first, wantErr) || second != nil || third != nil {
		t.Fatalf("Close retry/cache results: first=%v second=%v third=%v", first, second, third)
	}
	if calls != 2 {
		t.Fatalf("Close terminated process %d times, want one failure + one successful retry", calls)
	}
	if browserCtx.Err() == nil {
		t.Fatal("Close left browser context alive")
	}
}

func TestBrowserTerminate_ConnectNeverKills(t *testing.T) {
	calls := 0
	owned := newOwnedProcess(func(context.Context, *exec.Cmd, uint64) error {
		calls++
		return nil
	})
	owned.bind(exec.Command("chrome"))
	b := &Browser{attached: true, ownedProcess: owned}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := b.Terminate(ctx); err != nil {
		t.Fatalf("Terminate on Connect browser: %v", err)
	}
	if calls != 0 {
		t.Fatalf("Terminate on Connect called process killer %d times", calls)
	}
}

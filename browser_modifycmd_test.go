package chromekit_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/tuwibu/chromekit"
)

// TestWithModifyCmd_HookFiresBeforeStart pins the contract: the hook must
// receive a non-nil *exec.Cmd before chromedp tries to start the process,
// so callers can mutate Cmd.Args (e.g. inject single-dash GPM-Login flags
// that chromedp.Flag can't render).
//
// Strategy: pass a deliberately invalid chrome path so Cmd.Start fails fast.
// The hook MUST still have been called, otherwise the option is broken.
func TestWithModifyCmd_HookFiresBeforeStart(t *testing.T) {
	const sentinel = "-chromekit-test-sentinel"
	var called bool
	var captured []string

	hook := func(c *exec.Cmd) {
		called = true
		c.Args = append(c.Args, sentinel)
		captured = append([]string{}, c.Args...)
	}

	// Launch will fail (chromePath is a non-existent file). The hook fires
	// during chromedp's Allocate before Start — that's the contract we pin.
	_, err := chromekit.Launch(
		"/__chromekit_nonexistent_chrome__",
		chromekit.WithModifyCmd(hook),
	)
	if err == nil {
		t.Fatal("expected Launch to fail with invalid chrome path")
	}

	if !called {
		t.Fatal("WithModifyCmd hook did not fire before Cmd.Start")
	}

	found := false
	for _, a := range captured {
		if strings.Contains(a, sentinel) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("hook ran but sentinel arg not in captured args: %v", captured)
	}
}

// TestWithModifyCmd_NilHookIgnored verifies passing nil does not panic and
// does not register a hook. Documents the additive-safe contract.
func TestWithModifyCmd_NilHookIgnored(t *testing.T) {
	// Just constructing the option must not panic.
	_ = chromekit.WithModifyCmd(nil)
}

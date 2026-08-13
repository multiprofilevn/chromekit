package chromekit

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// In Connect mode chromekit is a guest in someone else's browser. Cancelling a
// page sub-context sends a real Target.closeTarget, so Close() must touch only
// the tabs chromekit opened — never the ones that were already there.

func TestClose_Attached_KeepsPreexistingTabs(t *testing.T) {
	bctx, bcancel := context.WithCancel(context.Background())
	defer bcancel()
	b := &Browser{log: noopLogger{}, cfg: defaultConfig(), ctx: bctx, cancel: bcancel, attached: true}

	// The tab Connect attached to: shares the browser context.
	primary := &Page{id: "primary", ctx: bctx, cancel: bcancel, primary: true, owned: false, browser: b}
	// A tab the user already had open when Connect ran.
	userCtx, userCancel := context.WithCancel(bctx)
	userTab := &Page{id: "user-tab", ctx: userCtx, cancel: userCancel, owned: false, browser: b}
	// A tab chromekit opened via NewPage.
	ourCtx, ourCancel := context.WithCancel(bctx)
	ourTab := &Page{id: "our-tab", ctx: ourCtx, cancel: ourCancel, owned: true, browser: b}
	b.pages = []*Page{primary, userTab, ourTab}
	b.current = primary

	b.Close()

	// Every context is cancelled — that is what releases the goroutines. Tabs
	// the user already had survive because Close detaches them from chromedp
	// first, so the cancellation cannot turn into a Target.closeTarget. That
	// half is only observable against a real browser; see
	// TestIntegration_ConnectClose_PreexistingTabsSurvive.
	for name, ctx := range map[string]context.Context{
		"user tab":        userCtx,
		"chromekit tab":   ourCtx,
		"browser/primary": bctx,
	} {
		if ctx.Err() == nil {
			t.Errorf("%s context still alive after Close() — its goroutines leak", name)
		}
	}
	_ = primary
	_ = userTab
	_ = ourTab
}

func TestClose_Launch_CancelsEverything(t *testing.T) {
	// The Launch path owns the process: teardown must be total, including the
	// primary page, or the Chrome process is orphaned.
	bctx, bcancel := context.WithCancel(context.Background())
	b := &Browser{log: noopLogger{}, cfg: defaultConfig(), ctx: bctx, cancel: bcancel, attached: false}

	primary := &Page{id: "primary", ctx: bctx, cancel: bcancel, primary: true, owned: true, browser: b}
	tabCtx, tabCancel := context.WithCancel(bctx)
	tab := &Page{id: "tab", ctx: tabCtx, cancel: tabCancel, owned: true, browser: b}
	b.pages = []*Page{primary, tab}
	b.current = primary

	b.Close()

	if bctx.Err() == nil {
		t.Fatal("Close() on the Launch path left the browser context alive — Chrome is orphaned")
	}
	if tabCtx.Err() == nil {
		t.Error("Close() left a tab context alive")
	}
}

func TestDone_ClosesAfterClose(t *testing.T) {
	// Done() used to follow only the chromedp context. The Connect path never
	// cancels that context, so anything waiting on <-b.Done() after Close()
	// blocked for the life of the process.
	for _, attached := range []bool{true, false} {
		name := "launch"
		if attached {
			name = "connect"
		}
		t.Run(name, func(t *testing.T) {
			bctx, bcancel := context.WithCancel(context.Background())
			defer bcancel()
			b := &Browser{log: noopLogger{}, cfg: defaultConfig(), ctx: bctx, cancel: bcancel, attached: attached}
			b.pages = []*Page{{id: "primary", ctx: bctx, cancel: bcancel, primary: true, owned: true, browser: b}}
			b.current = b.pages[0]

			b.Close()

			select {
			case <-b.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("Done() still open after Close()")
			}
		})
	}
}

func TestOwnedFlagWiring(t *testing.T) {
	bctx, bcancel := context.WithCancel(context.Background())
	defer bcancel()
	b := &Browser{log: noopLogger{}, cfg: defaultConfig(), ctx: bctx, cancel: bcancel, attached: true}

	b.pagesMu.Lock()
	preexisting, err := b.attachToTarget("was-already-open", false, false)
	b.pagesMu.Unlock()
	if err != nil {
		t.Fatalf("attachToTarget: %v", err)
	}
	if preexisting.owned {
		t.Error("a tab attached from Connect's target list is marked owned — Close() would shut the user's tab")
	}
}

func TestWithProcessGroupDefaults_PreservesUserHook(t *testing.T) {
	// ModifyCmdFunc replaces chromedp's own cmd hook. The wrapper must preserve
	// the caller's hook; platform-specific attribute behavior is pinned in the
	// browser_procattr_* tests.
	called := false
	hook := withProcessGroupDefaults(func(cmd *exec.Cmd) {
		called = true
		cmd.Args = append(cmd.Args, "--user-flag")
	})

	cmd := exec.Command("chrome")
	hook(cmd)

	if !called {
		t.Fatal("user hook not invoked")
	}
	found := false
	for _, a := range cmd.Args {
		if a == "--user-flag" {
			found = true
		}
	}
	if !found {
		t.Error("user hook's argument was lost")
	}
}

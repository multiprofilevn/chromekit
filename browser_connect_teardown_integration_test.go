//go:build integration

package chromekit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"testing"
	"time"
)

// Connect makes chromekit a guest in a browser it does not own. Close() must
// hand that browser back intact — same process, same tabs — while still
// releasing everything chromekit allocated.

const teardownProbePort = 9787

func startUserChrome(t *testing.T) {
	t.Helper()
	chromePath := resolveChromePath(t)
	cmd := exec.Command(chromePath,
		fmt.Sprintf("--remote-debugging-port=%d", teardownProbePort),
		`--user-data-dir=`+t.TempDir(),
		"--headless=new",
		"--no-first-run",
		"--no-default-browser-check",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start chrome: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if userChromeAlive() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("chrome never opened its debug port")
}

func userChromeAlive() bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", teardownProbePort))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func userChromeTabs(t *testing.T) []string {
	t.Helper()
	targets, err := fetchPageTargets(context.Background(), teardownProbePort)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(targets))
	for _, tg := range targets {
		ids = append(ids, tg.ID)
	}
	return ids
}

func openUserTab(t *testing.T) {
	t.Helper()
	c := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodPut,
		fmt.Sprintf("http://127.0.0.1:%d/json/new?about:blank", teardownProbePort), nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("open tab: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
}

func TestIntegration_ConnectClose_PreexistingTabsSurvive(t *testing.T) {
	startUserChrome(t)
	openUserTab(t) // the user now has two tabs of their own

	before := userChromeTabs(t)
	if len(before) < 2 {
		t.Fatalf("expected at least 2 user tabs before connecting, got %d", len(before))
	}

	b, err := Connect(teardownProbePort)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	ours, err := b.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	ourID := ours.ID()

	b.Close()
	time.Sleep(700 * time.Millisecond)

	if !userChromeAlive() {
		t.Fatal("Close() killed a Chrome process chromekit does not own")
	}
	after := userChromeTabs(t)

	for _, id := range before {
		found := false
		for _, a := range after {
			if a == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Close() closed the user's pre-existing tab %s", id)
		}
	}
	for _, a := range after {
		if a == ourID {
			t.Errorf("Close() left the chromekit-opened tab %s behind", ourID)
		}
	}
}

func TestIntegration_ConnectClose_ReleasesGoroutines(t *testing.T) {
	startUserChrome(t)

	// Warm-up cycle so lazily-created machinery is not counted as growth.
	warm, err := Connect(teardownProbePort)
	if err != nil {
		t.Fatalf("warmup connect: %v", err)
	}
	warm.Close()
	time.Sleep(500 * time.Millisecond)
	baseline := stableGoroutineCount()

	const cycles = 10
	for i := 0; i < cycles; i++ {
		b, err := Connect(teardownProbePort)
		if err != nil {
			t.Fatalf("cycle %d connect: %v", i, err)
		}
		if p := b.Current(); p != nil {
			_, _ = p.GetURL()
		}
		b.Close()
	}
	if !userChromeAlive() {
		t.Fatal("Chrome died over repeated Connect/Close cycles")
	}

	// Pre-fix this grew ~7 goroutines per cycle (~70 for 10 cycles).
	if !awaitGoroutinesAtMost(baseline+5, 10*time.Second) {
		t.Fatalf("goroutines grew from %d to %d over %d Connect/Close cycles",
			baseline, stableGoroutineCount(), cycles)
	}
}

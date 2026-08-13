//go:build windows

package chromekit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestTaskkillCommand_UsesTreeForcePIDArguments(t *testing.T) {
	cmd := taskkillCommand(context.Background(), 4242)
	want := []string{"taskkill.exe", "/T", "/F", "/PID", "4242"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("taskkill args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		got := cmd.Args[i]
		if i == 0 {
			got = filepath.Base(got)
		}
		if !strings.EqualFold(got, want[i]) {
			t.Fatalf("taskkill args = %v, want %v", cmd.Args, want)
		}
	}
}

func TestLaunchFailureTerminatesOwnedHelperTree(t *testing.T) {
	mode := os.Getenv("CHROMEKIT_TREE_HELPER")
	if mode != "" {
		runTreeHelper(t, mode)
		return
	}

	pidFile := t.TempDir() + `\pids.txt`
	_, err := Launch(os.Args[0],
		WithWSURLReadTimeout(300*time.Millisecond),
		WithModifyCmd(func(cmd *exec.Cmd) {
			cmd.Args = []string{os.Args[0], "-test.run=TestLaunchFailureTerminatesOwnedHelperTree"}
			cmd.Env = append(os.Environ(),
				"CHROMEKIT_TREE_HELPER=parent",
				"CHROMEKIT_TREE_PID_FILE="+pidFile,
			)
		}),
	)
	if err == nil {
		t.Fatal("Launch unexpectedly succeeded")
	}

	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("read helper pids after Launch failure: %v", readErr)
	}
	for _, raw := range strings.Fields(string(data)) {
		pid64, convErr := strconv.ParseUint(raw, 10, 32)
		if convErr != nil {
			t.Fatalf("parse helper pid %q: %v", raw, convErr)
		}
		if !waitProcessExit(uint32(pid64), 5*time.Second) {
			t.Errorf("helper process %d survived failed Launch teardown", pid64)
		}
	}
}

func TestNaturalDoneTerminatesSurvivingHelper(t *testing.T) {
	mode := os.Getenv("CHROMEKIT_NATURAL_DONE_HELPER")
	if mode != "" {
		runNaturalDoneHelper(t, mode)
		return
	}

	pidFile := t.TempDir() + `\child-pid.txt`
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestNaturalDoneTerminatesSurvivingHelper$")
	cmd.Env = append(os.Environ(),
		"CHROMEKIT_NATURAL_DONE_HELPER=parent",
		"CHROMEKIT_TREE_PID_FILE="+pidFile,
	)
	owned := newOwnedProcess(terminateOwnedProcessTree)
	owned.bind(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start natural-exit root: %v", err)
	}
	if err := owned.rememberIdentity(); err != nil {
		t.Fatalf("remember root identity: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("natural-exit root wait: %v", err)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read surviving helper pid: %v", err)
	}
	childPID64, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
	if err != nil {
		t.Fatalf("parse surviving helper pid: %v", err)
	}
	childPID := uint32(childPID64)
	t.Cleanup(func() { _ = taskkillCommand(context.Background(), childPID).Run() })
	if !processAlive(childPID) {
		t.Fatal("natural-exit fixture child did not survive its parent")
	}

	browserCtx, browserCancel := context.WithCancel(context.Background())
	b := &Browser{
		ctx:          browserCtx,
		cancel:       browserCancel,
		ownedProcess: owned,
	}
	go b.cleanupOwnedProcessOnContextDone()
	deathStarted := time.Now()
	browserCancel()

	select {
	case <-b.Done():
		if elapsed := time.Since(deathStarted); elapsed >= 100*time.Millisecond {
			t.Fatalf("Done reported natural browser death after %s, want <100ms", elapsed)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Done did not report natural browser death within 100ms")
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close natural-exit cleanup barrier: %v", err)
	}
	if processAlive(childPID) {
		t.Fatalf("helper process %d survived natural Browser.Close cleanup barrier", childPID)
	}
}

func runTreeHelper(t *testing.T, mode string) {
	t.Helper()
	if mode == "child" {
		time.Sleep(30 * time.Second)
		return
	}

	pidFile := os.Getenv("CHROMEKIT_TREE_PID_FILE")
	child := exec.Command(os.Args[0], "-test.run=TestLaunchFailureTerminatesOwnedHelperTree")
	child.Env = append(os.Environ(), "CHROMEKIT_TREE_HELPER=child")
	if err := child.Start(); err != nil {
		t.Fatalf("start tree child: %v", err)
	}
	contents := fmt.Sprintf("%d %d", os.Getpid(), child.Process.Pid)
	if err := os.WriteFile(pidFile, []byte(contents), 0o600); err != nil {
		t.Fatalf("write helper pids: %v", err)
	}
	time.Sleep(30 * time.Second)
}

func runNaturalDoneHelper(t *testing.T, mode string) {
	t.Helper()
	if mode == "child" {
		time.Sleep(30 * time.Second)
		return
	}

	child := exec.Command(os.Args[0], "-test.run=^TestNaturalDoneTerminatesSurvivingHelper$")
	child.Env = append(os.Environ(), "CHROMEKIT_NATURAL_DONE_HELPER=child")
	if err := child.Start(); err != nil {
		t.Fatalf("start natural-exit child: %v", err)
	}
	pidFile := os.Getenv("CHROMEKIT_TREE_PID_FILE")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		t.Fatalf("write natural-exit child pid: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
}

func processAlive(pid uint32) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	state, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && state != windows.WAIT_OBJECT_0
}

func waitProcessExit(pid uint32, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for processAlive(pid) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(25 * time.Millisecond):
		}
	}
	return true
}

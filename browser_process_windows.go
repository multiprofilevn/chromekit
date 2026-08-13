//go:build windows

package chromekit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const taskkillAttemptTimeout = 2 * time.Second

type windowsProcessRef struct {
	pid       uint32
	createdAt uint64
	handle    windows.Handle
}

func terminateOwnedProcessTree(ctx context.Context, cmd *exec.Cmd, expectedIdentity uint64) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	rootPID := uint32(cmd.Process.Pid)
	rootCreatedAt, rootAlive, identityErr := currentWindowsProcessIdentity(cmd.Process)
	if identityErr != nil {
		if expectedIdentity == 0 {
			return fmt.Errorf("inspect owned root process %d: %w", rootPID, identityErr)
		}
		// Wait may already have released the root handle. The identity captured
		// immediately after Start still lets Toolhelp distinguish an exited root
		// from a newly-reused PID below.
		rootCreatedAt = expectedIdentity
		rootAlive = false
	}
	if rootAlive && expectedIdentity != 0 && rootCreatedAt != expectedIdentity {
		return fmt.Errorf("owned root process %d identity changed", rootPID)
	}
	if expectedIdentity == 0 {
		expectedIdentity = rootCreatedAt
	}
	refs, snapshotErr := captureWindowsProcessTree(rootPID)
	defer func() { closeWindowsProcessRefs(refs) }()
	rootRefFound, rootRefMatches := windowsRootProcessIdentity(refs, rootPID, expectedIdentity)
	if rootRefFound && !rootRefMatches {
		rootErr := cmd.Process.Kill()
		if errors.Is(rootErr, os.ErrProcessDone) {
			return nil
		}
		return errors.Join(
			fmt.Errorf("refuse numeric tree kill for unverified root process %d", rootPID),
			snapshotErr,
			rootErr,
		)
	}
	rootAlive = rootAlive || rootRefMatches
	refs = filterWindowsProcessRefsCreatedAfter(refs, expectedIdentity)

	var firstOutput []byte
	var firstErr error
	if rootAlive {
		firstOutput, firstErr = runTaskkill(ctx, rootPID)
	}
	if firstErr == nil && snapshotErr == nil && waitWindowsProcessRefs(ctx, refs, 350*time.Millisecond) {
		return nil
	}

	// taskkill can fail under transient process churn. Re-snapshot while the
	// root identity is still live, then terminate the captured handles from
	// leaves to root so PID reuse cannot redirect the fallback.
	var retrySnapshotErr error
	if windowsProcessRefAlive(refs, rootPID) {
		var retryRefs []windowsProcessRef
		retryRefs, retrySnapshotErr = captureWindowsProcessTree(rootPID)
		refs = append(refs, retryRefs...)
	}
	for i := len(refs) - 1; i >= 0; i-- {
		if refs[i].pid != rootPID {
			_ = windows.TerminateProcess(refs[i].handle, 1)
		}
	}

	var retryOutput []byte
	var retryErr error
	if windowsProcessRefAlive(refs, rootPID) {
		retryOutput, retryErr = runTaskkill(ctx, rootPID)
	}
	for i := len(refs) - 1; i >= 0; i-- {
		if refs[i].pid == rootPID {
			_ = windows.TerminateProcess(refs[i].handle, 1)
		}
	}

	// os.Process.Kill uses the handle retained by exec.Cmd, making it the safe
	// final root fallback even if the numeric PID has already been recycled.
	rootErr := cmd.Process.Kill()
	if errors.Is(rootErr, os.ErrProcessDone) {
		rootErr = nil
	}
	if snapshotErr == nil && retrySnapshotErr == nil && waitWindowsProcessRefs(ctx, refs, 0) {
		return nil
	}

	verificationErr := ctx.Err()
	if verificationErr == nil {
		verificationErr = errors.New("bounded process-tree verification failed")
	}
	return fmt.Errorf(
		"process tree %d survived teardown: snapshot=%v retry_snapshot=%v taskkill=%v (%s) root_kill=%v retry=%v (%s): %w",
		rootPID,
		snapshotErr,
		retrySnapshotErr,
		firstErr,
		strings.TrimSpace(string(firstOutput)),
		rootErr,
		retryErr,
		strings.TrimSpace(string(retryOutput)),
		verificationErr,
	)
}

func taskkillCommand(ctx context.Context, pid uint32) *exec.Cmd {
	taskkill := "taskkill.exe"
	if systemDir, err := windows.GetSystemDirectory(); err == nil {
		taskkill = filepath.Join(systemDir, taskkill)
	}
	cmd := exec.CommandContext(ctx, taskkill, "/T", "/F", "/PID", strconv.FormatUint(uint64(pid), 10))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd
}

func runTaskkill(ctx context.Context, pid uint32) ([]byte, error) {
	attemptCtx, cancel := boundedProcessContext(ctx, taskkillAttemptTimeout)
	defer cancel()
	cmd := taskkillCommand(attemptCtx, pid)
	cmd.WaitDelay = time.Second
	return cmd.CombinedOutput()
}

//go:build windows

package chromekit

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func boundedProcessContext(parent context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= limit {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, limit)
}

func closeWindowsProcessRefs(refs []windowsProcessRef) {
	for _, ref := range refs {
		windows.CloseHandle(ref.handle)
	}
}

func waitWindowsProcessRefs(ctx context.Context, refs []windowsProcessRef, grace time.Duration) bool {
	waitCtx := ctx
	cancel := func() {}
	if grace > 0 {
		waitCtx, cancel = boundedProcessContext(ctx, grace)
	}
	defer cancel()

	for {
		allExited := true
		for _, ref := range refs {
			state, err := windows.WaitForSingleObject(ref.handle, 0)
			if err != nil || state != windows.WAIT_OBJECT_0 {
				allExited = false
				break
			}
		}
		if allExited {
			return true
		}
		select {
		case <-waitCtx.Done():
			return false
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func windowsProcessRefAlive(refs []windowsProcessRef, pid uint32) bool {
	for _, ref := range refs {
		if ref.pid != pid {
			continue
		}
		state, err := windows.WaitForSingleObject(ref.handle, 0)
		return err == nil && state != windows.WAIT_OBJECT_0
	}
	return false
}

func windowsRootProcessIdentity(refs []windowsProcessRef, pid uint32, createdAt uint64) (found, matches bool) {
	for _, ref := range refs {
		if ref.pid == pid {
			return true, ref.createdAt == createdAt
		}
	}
	return false, false
}

func filterWindowsProcessRefsCreatedAfter(refs []windowsProcessRef, rootCreatedAt uint64) []windowsProcessRef {
	kept := refs[:0]
	for _, ref := range refs {
		if rootCreatedAt != 0 && ref.createdAt < rootCreatedAt {
			windows.CloseHandle(ref.handle)
			continue
		}
		kept = append(kept, ref)
	}
	return kept
}

func ownedProcessIdentity(process *os.Process) (uint64, error) {
	createdAt, _, err := currentWindowsProcessIdentity(process)
	return createdAt, err
}

func currentWindowsProcessIdentity(process *os.Process) (createdAt uint64, alive bool, err error) {
	var inspectErr error
	handleErr := process.WithHandle(func(rawHandle uintptr) {
		handle := windows.Handle(rawHandle)
		state, waitErr := windows.WaitForSingleObject(handle, 0)
		if waitErr != nil {
			inspectErr = waitErr
			return
		}
		if state == windows.WAIT_OBJECT_0 {
			return
		}

		var creationTime, exitTime, kernelTime, userTime windows.Filetime
		if timesErr := windows.GetProcessTimes(handle, &creationTime, &exitTime, &kernelTime, &userTime); timesErr != nil {
			inspectErr = timesErr
			return
		}
		createdAt = uint64(creationTime.Nanoseconds())
		alive = true
	})
	if errors.Is(handleErr, os.ErrProcessDone) || errors.Is(handleErr, os.ErrNoHandle) {
		return 0, false, nil
	}
	return createdAt, alive, errors.Join(handleErr, inspectErr)
}

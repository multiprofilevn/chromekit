//go:build windows

package chromekit

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func captureWindowsProcessTree(rootPID uint32) ([]windowsProcessRef, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	children := make(map[uint32][]uint32)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	for {
		children[entry.ParentProcessID] = append(children[entry.ParentProcessID], entry.ProcessID)
		entry.Size = uint32(unsafe.Sizeof(windows.ProcessEntry32{}))
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}

	pids := descendantProcessIDs(rootPID, children)
	refs := make([]windowsProcessRef, 0, len(pids))
	var captureErr error
	for _, pid := range pids {
		handle, err := windows.OpenProcess(
			windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE,
			false,
			pid,
		)
		if err != nil {
			// A process can legitimately exit between the snapshot and OpenProcess.
			// Other failures (notably access denied) mean we cannot verify that the
			// whole captured tree is gone, so preserve them for the caller.
			if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) && !errors.Is(err, windows.ERROR_NOT_FOUND) {
				captureErr = errors.Join(captureErr, fmt.Errorf("open process %d: %w", pid, err))
			}
			continue
		}
		var creationTime, exitTime, kernelTime, userTime windows.Filetime
		if err := windows.GetProcessTimes(handle, &creationTime, &exitTime, &kernelTime, &userTime); err != nil {
			windows.CloseHandle(handle)
			captureErr = errors.Join(captureErr, fmt.Errorf("get process %d identity: %w", pid, err))
			continue
		}
		refs = append(refs, windowsProcessRef{
			pid:       pid,
			createdAt: uint64(creationTime.Nanoseconds()),
			handle:    handle,
		})
	}
	return refs, captureErr
}

func descendantProcessIDs(rootPID uint32, children map[uint32][]uint32) []uint32 {
	pids := []uint32{rootPID}
	visited := map[uint32]bool{rootPID: true}
	for i := 0; i < len(pids); i++ {
		for _, childPID := range children[pids[i]] {
			if childPID == pids[i] || visited[childPID] {
				continue
			}
			visited[childPID] = true
			pids = append(pids, childPID)
		}
	}
	return pids
}

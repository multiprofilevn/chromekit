//go:build !windows

package chromekit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func ownedProcessIdentity(*os.Process) (uint64, error) { return 0, nil }

func terminateOwnedProcessTree(ctx context.Context, cmd *exec.Cmd, _ uint64) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	pid := cmd.Process.Pid
	groupErr := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(groupErr, syscall.ESRCH) {
		groupErr = nil
	}
	rootErr := cmd.Process.Kill()
	if errors.Is(rootErr, os.ErrProcessDone) {
		rootErr = nil
	}
	return errors.Join(groupErr, rootErr)
}

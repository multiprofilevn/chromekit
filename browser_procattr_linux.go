//go:build linux

package chromekit

import (
	"os"
	"os/exec"
	"syscall"
)

func applyProcessGroupAttrs(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// applyDefaultProcessAttrs mirrors chromedp's Linux allocator behavior when
// no user hook is supplied, preserving the existing parent-death contract.
func applyDefaultProcessAttrs(cmd *exec.Cmd) {
	if _, ok := os.LookupEnv("LAMBDA_TASK_ROOT"); ok {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

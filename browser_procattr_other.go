//go:build !windows && !linux

package chromekit

import (
	"os/exec"
	"syscall"
)

// applyProcessGroupAttrs restores what chromedp's default cmd hook would have
// set: Chrome in its own process group, so a Ctrl+C aimed at the Go parent
// does not tear the browser down mid-operation. Only fills in what the caller
// has not already chosen.
func applyProcessGroupAttrs(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// chromedp's default allocator hook is a no-op outside Linux.
func applyDefaultProcessAttrs(*exec.Cmd) {}

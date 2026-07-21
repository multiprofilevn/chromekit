//go:build windows

package chromekit

import (
	"os/exec"
	"syscall"
)

// createNewProcessGroup mirrors CREATE_NEW_PROCESS_GROUP. Chrome in its own
// group does not receive the console Ctrl+C that kills the Go parent, so the
// parent gets to shut it down deliberately instead of both dying half-way.
const createNewProcessGroup = 0x00000200

// applyProcessGroupAttrs restores what chromedp's default cmd hook would have
// set. Only fills in what the caller has not already chosen.
func applyProcessGroupAttrs(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNewProcessGroup
}

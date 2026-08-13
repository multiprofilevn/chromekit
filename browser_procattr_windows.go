//go:build windows

package chromekit

import (
	"os/exec"
)

// createNewProcessGroup mirrors CREATE_NEW_PROCESS_GROUP for callers that
// explicitly opt in through WithModifyCmd.
const createNewProcessGroup = 0x00000200

// applyProcessGroupAttrs intentionally does nothing on Windows. In particular,
// WithModifyCmd must not silently add CREATE_NEW_PROCESS_GROUP: callers that
// need it can opt in through cmd.SysProcAttr in their hook.
func applyProcessGroupAttrs(*exec.Cmd) {}

// chromedp's default allocator hook is also a no-op on Windows.
func applyDefaultProcessAttrs(*exec.Cmd) {}

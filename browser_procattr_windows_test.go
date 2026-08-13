//go:build windows

package chromekit

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestWithProcessGroupDefaults_WindowsDoesNotImplicitlyCreateGroup(t *testing.T) {
	cmd := exec.Command("chrome")
	withProcessGroupDefaults(func(*exec.Cmd) {})(cmd)

	if cmd.SysProcAttr != nil && cmd.SysProcAttr.CreationFlags&createNewProcessGroup != 0 {
		t.Fatal("WithModifyCmd implicitly set CREATE_NEW_PROCESS_GROUP on Windows")
	}
}

func TestWithProcessGroupDefaults_WindowsPreservesExplicitGroupOptIn(t *testing.T) {
	cmd := exec.Command("chrome")
	withProcessGroupDefaults(func(cmd *exec.Cmd) {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
	})(cmd)

	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&createNewProcessGroup == 0 {
		t.Fatal("WithModifyCmd discarded caller's explicit CREATE_NEW_PROCESS_GROUP opt-in")
	}
}

//go:build !windows

package chromekit

import (
	"os/exec"
	"testing"
)

func TestWithProcessGroupDefaults_NonWindowsKeepsProcessGroup(t *testing.T) {
	cmd := exec.Command("chrome")
	withProcessGroupDefaults(func(*exec.Cmd) {})(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("WithModifyCmd no longer restores the non-Windows process group")
	}
}

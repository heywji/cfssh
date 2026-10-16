//go:build windows

package hidewin

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestApplyKeepsFlagsAndHides(t *testing.T) {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	Apply(cmd)
	f := cmd.SysProcAttr.CreationFlags
	if f&createNoWindow == 0 || f&syscall.CREATE_NEW_PROCESS_GROUP == 0 || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("flags=%#x hide=%v", f, cmd.SysProcAttr.HideWindow)
	}
}

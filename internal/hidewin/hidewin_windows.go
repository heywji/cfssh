//go:build windows

package hidewin

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// Apply suppresses the console window of cmd, keeping any flags already set.
func Apply(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

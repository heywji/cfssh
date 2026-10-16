//go:build !windows

package hidewin

import "os/exec"

// Apply does nothing outside Windows: there is no console window to hide.
func Apply(*exec.Cmd) {}

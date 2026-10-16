// Package hidewin starts console programs without a console window.
//
// cfssh.exe is a GUI program (-H windowsgui), so on Windows every console child
// it starts — sc.exe, powershell.exe, icacls.exe, ssh-keygen.exe, cloudflared —
// gets a brand-new console window. The helper polls `sc.exe query sshd` every
// second, which flashed a black window every second, and cloudflared kept one
// open for the whole session. Route every child through Command (or Apply).
package hidewin

import "os/exec"

// Command is exec.Command with the console window suppressed on Windows.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	Apply(cmd)
	return cmd
}

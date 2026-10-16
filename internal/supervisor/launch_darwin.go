//go:build darwin

package supervisor

import (
	"os"
	"os/exec"
	"strings"
)

// StartHelper launches the privileged helper. On macOS this asks for an
// administrator password once (osascript). The returned command stays running until stop.
func StartHelper(dir string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	if os.Geteuid() == 0 {
		cmd = exec.Command(exe, "helper", "-dir", dir)
	} else {
		inner := shellQuote(exe) + " helper -dir " + shellQuote(dir)
		script := `do shell script "` + escapeApple(inner) + `" with administrator privileges`
		cmd = exec.Command("/usr/bin/osascript", "-e", script)
	}
	log, err := os.OpenFile(launchLog(dir), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, err
	}
	go func() {
		_ = cmd.Wait()
		_ = log.Close()
	}()
	return cmd, nil
}

func launchLog(dir string) string { return dir + "/launch.log" }

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func escapeApple(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

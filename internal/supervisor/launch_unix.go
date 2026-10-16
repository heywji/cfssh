//go:build !windows

package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// StartHelper launches the privileged helper. On Linux and macOS this asks for
// an administrator password once. The returned command stays running until stop.
func StartHelper(dir string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	if os.Geteuid() == 0 {
		cmd = exec.Command(exe, "helper", "-dir", dir)
	} else if _, err := exec.LookPath("osascript"); err == nil && isDarwin() {
		inner := shellQuote(exe) + " helper -dir " + shellQuote(dir)
		script := `do shell script "` + escapeApple(inner) + `" with administrator privileges`
		cmd = exec.Command("osascript", "-e", script)
	} else if path, err := exec.LookPath("pkexec"); err == nil {
		cmd = exec.Command(path, exe, "helper", "-dir", dir)
	} else if path, err := exec.LookPath("sudo"); err == nil {
		cmd = exec.Command(path, exe, "helper", "-dir", dir)
	} else {
		return nil, fmt.Errorf("需要 pkexec、sudo，或在 macOS 上使用 osascript 来获取管理员权限")
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

func isDarwin() bool { return goos() == "darwin" }

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func escapeApple(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

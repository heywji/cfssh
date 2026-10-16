//go:build windows

package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func StartHelper(dir string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "helper", "-dir", dir)
	log, err := os.OpenFile(dir+`\launch.log`, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
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

// PrepareProcess relaunches the GUI with a UAC prompt when it is not already
// an administrator. SSH installation and the Windows OpenSSH service need that.
func PrepareProcess() error {
	if admin() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	cwdPtr, _ := windows.UTF16PtrFromString(cwd)
	arg := strings.Join(os.Args[1:], " ")
	var argPtr *uint16
	if arg != "" {
		argPtr, _ = windows.UTF16PtrFromString(arg)
	}
	err = windows.ShellExecute(0, verb, file, argPtr, cwdPtr, windows.SW_NORMAL)
	if err != nil {
		return fmt.Errorf("请求管理员权限失败: %w", err)
	}
	os.Exit(0)
	return nil
}

func admin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		// Fall back to an elevated token on the current process.
		var process windows.Token
		if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &process); err != nil {
			return false
		}
		defer process.Close()
		member, err = process.IsMember(sid)
		if err != nil {
			return false
		}
	}
	return member
}

// Silence unused syscall import if the compiler drops it. ShellExecute lives in x/sys.
var _ = syscall.Errno(0)

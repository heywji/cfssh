//go:build linux

package auth

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"

	"cfssh/internal/clientcfg"
)

func verify(password string) error {
	name := clientcfg.CurrentUser()
	cmd := exec.Command("su", name, "-c", "true")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	tty, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("无法核对当前用户密码: %w", err)
	}
	defer tty.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	time.Sleep(200 * time.Millisecond)
	_, _ = tty.Write([]byte(password + "\n"))
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("这个密码不能登录 Linux 用户 %s。客户端 SSH 时要填同一个密码", name)
		}
		return nil
	case <-time.After(8 * time.Second):
		_ = cmd.Process.Kill()
		return fmt.Errorf("核对当前用户 %s 的密码超时", name)
	}
}

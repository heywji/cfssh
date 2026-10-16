//go:build darwin

package auth

import (
	"fmt"
	"os/exec"

	"cfssh/internal/clientcfg"
)

func verify(password string) error {
	name := clientcfg.CurrentUser()
	cmd := exec.Command("dscl", ".", "-authonly", name, password)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("这个密码不能登录 macOS 用户 %s。客户端 SSH 时要填同一个密码", name)
	}
	return nil
}

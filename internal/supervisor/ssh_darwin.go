//go:build darwin

package supervisor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cfssh/internal/sshd"
)

// macOS ships /usr/sbin/sshd. cfssh runs its own sshd with its own config on
// 127.0.0.1 and never touches the system Remote Login service.
const (
	darwinSSHD       = "/usr/sbin/sshd"
	darwinSFTP       = "/usr/libexec/sftp-server"
	darwinConfigPath = "/etc/ssh/cfssh_sshd_config"
)

func startSSHD(dir string, req Request, logf func(string)) (*proc, error) {
	if !fileExists(darwinSSHD) {
		return nil, fmt.Errorf("找不到 %s", darwinSSHD)
	}
	reapPrevious(dir, "sshd.pid", "cfssh_sshd_config", logf)
	keys, err := ensureHostKeys()
	if err != nil {
		return nil, err
	}
	sftp := ""
	if fileExists(darwinSFTP) {
		sftp = darwinSFTP
	}
	cfg := sshd.UnixConfig(sshd.UnixOptions{
		Port:         req.Port,
		Listen:       req.Listen,
		HostKeys:     keys,
		SFTP:         sftp,
		AllowUser:    req.AllowUser,
		PasswordAuth: req.PasswordAuth,
		PermitRoot:   req.PermitRoot,
	})
	if err := os.WriteFile(darwinConfigPath, []byte(cfg), 0o644); err != nil {
		return nil, fmt.Errorf("写入 %s 失败: %w", darwinConfigPath, err)
	}
	cmd := exec.Command(darwinSSHD, "-D", "-e", "-f", darwinConfigPath)
	configureCommand(cmd)
	if err := attachLog(cmd, logf); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(darwinConfigPath)
		return nil, err
	}
	pidFile := filepath.Join(dir, "sshd.pid")
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	p := &proc{
		cmd: cmd,
		stopFn: func() {
			stopProcess(cmd)
			_ = os.Remove(darwinConfigPath)
			_ = os.Remove(pidFile)
		},
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(req.Port))
	if err := waitListen(addr, 15*time.Second, p.alive); err != nil {
		p.stop()
		return nil, fmt.Errorf("sshd 没有开始监听 %s: %w", addr, err)
	}
	logf("sshd 已监听 " + req.Listen + ":" + strconv.Itoa(req.Port))
	return p, nil
}

func ensureHostKeys() ([]string, error) {
	bin, err := exec.LookPath("ssh-keygen")
	if err != nil {
		return nil, fmt.Errorf("找不到 ssh-keygen")
	}
	ed := "/etc/ssh/cfssh_host_ed25519_key"
	rsa := "/etc/ssh/cfssh_host_rsa_key"
	if err := genKey(bin, ed, "ed25519", nil); err != nil {
		return nil, err
	}
	if err := genKey(bin, rsa, "rsa", []string{"-b", "3072"}); err != nil {
		return nil, err
	}
	return []string{ed, rsa}, nil
}

func genKey(bin, path, kind string, extra []string) error {
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		return os.Chmod(path, 0o600)
	}
	args := []string{"-t", kind, "-f", path, "-N", "", "-q"}
	args = append(args, extra...)
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("生成主机密钥 %s 失败: %w %s", path, err, strings.TrimSpace(string(out)))
	}
	return os.Chmod(path, 0o600)
}

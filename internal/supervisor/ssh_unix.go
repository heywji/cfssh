//go:build !windows

package supervisor

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cfssh/internal/sshd"
)

const unixConfigPath = "/etc/ssh/cfssh_sshd_config"

func startSSHD(dir string, req Request, logf func(string)) (*proc, error) {
	if err := ensureOpenSSH(dir, req.ParentPID, logf); err != nil {
		return nil, err
	}
	reapPrevious(dir, "sshd.pid", "cfssh_sshd_config", logf)
	keys, err := ensureHostKeys()
	if err != nil {
		return nil, err
	}
	allowSELinuxPort(req.Port, logf)
	bin, err := sshdBinary(logf)
	if err != nil {
		return nil, err
	}
	cfg := sshd.UnixConfig(sshd.UnixOptions{
		Port:         req.Port,
		Listen:       req.Listen,
		HostKeys:     keys,
		SFTP:         findSFTP(),
		AllowUser:    req.AllowUser,
		PasswordAuth: req.PasswordAuth,
		PermitRoot:   req.PermitRoot,
	})
	if err := os.WriteFile(unixConfigPath, []byte(cfg), 0o644); err != nil {
		return nil, fmt.Errorf("写入 %s 失败: %w", unixConfigPath, err)
	}
	if path, err := exec.LookPath("restorecon"); err == nil {
		_ = exec.Command(path, "-F", unixConfigPath).Run()
	}
	cmd := exec.Command(bin, "-D", "-e", "-f", unixConfigPath)
	configureCommand(cmd)
	if err := attachLog(cmd, logf); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(unixConfigPath)
		return nil, err
	}
	pidFile := filepath.Join(dir, "sshd.pid")
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	p := &proc{
		cmd: cmd,
		stopFn: func() {
			stopProcess(cmd)
			_ = os.Remove(unixConfigPath)
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

func ensureOpenSSH(dir string, parent int, logf func(string)) error {
	if findSSHD() != "" {
		return nil
	}
	logf("没有找到 sshd，正在安装 OpenSSH 服务器")
	var cmd *exec.Cmd
	switch {
	case fileExists("/usr/bin/apt-get"):
		cmd = exec.Command("bash", "-c", "export DEBIAN_FRONTEND=noninteractive; apt-get update && apt-get install -y openssh-server")
	case fileExists("/usr/bin/dnf"):
		cmd = exec.Command("dnf", "install", "-y", "openssh-server")
	case fileExists("/usr/bin/yum"):
		cmd = exec.Command("yum", "install", "-y", "openssh-server")
	default:
		return fmt.Errorf("找不到 sshd。请先安装 openssh-server")
	}
	out, err := combinedOutputWhileParent(parent, dir, cmd)
	text := strings.TrimSpace(string(out))
	if text != "" {
		logf(text)
	}
	if err != nil {
		return fmt.Errorf("安装 OpenSSH 失败: %w", err)
	}
	// A fresh package enables the system service on all interfaces.
	// This app runs its own sshd and stops it when the window closes.
	_ = exec.Command("systemctl", "disable", "--now", "ssh").Run()
	_ = exec.Command("systemctl", "disable", "--now", "sshd").Run()
	logf("已安装 OpenSSH，并关闭了安装程序启用的系统服务")
	if findSSHD() == "" {
		return fmt.Errorf("安装后仍然找不到 sshd")
	}
	return nil
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
	if path, err := exec.LookPath("restorecon"); err == nil {
		_ = exec.Command(path, "-F", ed, ed+".pub", rsa, rsa+".pub").Run()
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

func allowSELinuxPort(port int, logf func(string)) {
	out, err := exec.Command("getenforce").Output()
	if err != nil || !strings.Contains(string(out), "Enforcing") {
		return
	}
	bin, err := exec.LookPath("semanage")
	if err != nil {
		logf("SELinux 处于 Enforcing，但没有 semanage。若 sshd 起不来，请安装 policycoreutils-python-utils。")
		return
	}
	cmd := exec.Command(bin, "port", "-a", "-t", "ssh_port_t", "-p", "tcp", strconv.Itoa(port))
	msg, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(msg)), "already") {
		logf("semanage: " + strings.TrimSpace(string(msg)))
	}
}

func selinuxEnforcing() bool {
	out, err := exec.Command("getenforce").Output()
	return err == nil && strings.Contains(string(out), "Enforcing")
}

func sshdBinary(logf func(string)) (string, error) {
	src := findSSHD()
	if src == "" {
		return "", fmt.Errorf("找不到 sshd")
	}
	if selinuxEnforcing() {
		return src, nil
	}
	if _, err := os.Stat("/etc/apparmor.d/usr.sbin.sshd"); err != nil {
		return src, nil
	}
	dstDir := "/usr/local/libexec/cfssh"
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dstDir, "sshd")
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	logf("检测到 AppArmor。使用独立 sshd 副本，避免改系统策略，也避免占用系统 SSH 服务。")
	return dst, nil
}

func findSSHD() string {
	for _, p := range []string{"/usr/sbin/sshd", "/usr/bin/sshd", "/usr/local/sbin/sshd"} {
		if fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath("sshd"); err == nil {
		return p
	}
	return ""
}

func findSFTP() string {
	for _, p := range []string{
		"/usr/lib/openssh/sftp-server",
		"/usr/libexec/openssh/sftp-server",
		"/usr/libexec/sftp-server",
		"/usr/lib/ssh/sftp-server",
	} {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

package clientcfg

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EnsureKey creates ~/.ssh/cfssh_ed25519 when it is missing and installs the
// public key into the local authorized_keys file. The private key stays on disk.
func EnsureKey(logf func(string)) (string, error) {
	priv, pub, err := KeyPaths()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(priv)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	if _, err := os.Stat(priv); err != nil {
		bin, lookErr := exec.LookPath("ssh-keygen")
		if lookErr != nil {
			return "", fmt.Errorf("找不到 ssh-keygen，跳过密钥生成: %w", lookErr)
		}
		cmd := exec.Command(bin, "-t", "ed25519", "-f", priv, "-N", "", "-C", "cfssh", "-q")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("生成 SSH 密钥失败: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		if err := os.Chmod(priv, 0o600); err != nil {
			return "", err
		}
		logf("已生成本机密钥 " + priv)
	}
	pubLine, err := os.ReadFile(pub)
	if err != nil {
		return priv, err
	}
	if err := installAuthorizedKey(bytes.TrimSpace(pubLine)); err != nil {
		return priv, err
	}
	logf("公钥已写入本机 authorized_keys")
	return priv, nil
}

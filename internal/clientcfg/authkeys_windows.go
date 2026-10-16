//go:build windows

package clientcfg

import (
	"os"
	"path/filepath"
	"strings"

	"cfssh/internal/hidewin"
)

func installAuthorizedKey(pubLine []byte) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err := appendKeyFile(filepath.Join(home, ".ssh", "authorized_keys"), pubLine); err != nil {
		return err
	}
	// Windows OpenSSH ignores ~/.ssh/authorized_keys for administrators and
	// reads this file instead. The process is already elevated.
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	adminFile := filepath.Join(programData, "ssh", "administrators_authorized_keys")
	if err := appendKeyFile(adminFile, pubLine); err != nil {
		return err
	}
	// sshd rejects the admin key file unless only Administrators and SYSTEM can read it.
	cmd := hidewin.Command("icacls.exe", adminFile, "/inheritance:r", "/grant", "Administrators:F", "/grant", "SYSTEM:F")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errString("设置管理员公钥权限失败", err, out)
	}
	return nil
}

func appendKeyFile(path string, pubLine []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	line := string(pubLine)
	if line == "" || strings.Contains(string(existing), line) {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(line + "\n")
	return err
}

func errString(prefix string, err error, out []byte) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return err
	}
	return &wrap{prefix, msg, err}
}

type wrap struct {
	prefix string
	msg    string
	err    error
}

func (w *wrap) Error() string { return w.prefix + ": " + w.msg }
func (w *wrap) Unwrap() error { return w.err }

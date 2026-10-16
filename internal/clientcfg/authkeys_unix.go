//go:build !windows

package clientcfg

import (
	"os"
	"path/filepath"
	"strings"
)

func installAuthorizedKey(pubLine []byte) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return appendKeyFile(filepath.Join(home, ".ssh", "authorized_keys"), pubLine)
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
	if line == "" {
		return nil
	}
	if strings.Contains(string(existing), line) {
		return os.Chmod(path, 0o600)
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
	if _, err := f.WriteString(line + "\n"); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// Package provision defines the sidecar bundle an operator hands to a customer
// alongside the cfssh binary. It carries everything the customer machine needs
// to bring up the SSH tunnel — the Cloudflare tunnel token, the public hostname
// and the SSH port — but never the Cloudflare API token. The customer only types
// their own login password; all Cloudflare provisioning happened ahead of time.
package provision

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"cfssh/internal/config"
)

// Name is the sidecar filename looked for next to the executable.
const Name = "cfssh-provision.json"

// File is the provisioning bundle written by `cfssh provision` and read by the
// customer GUI. It contains no Cloudflare API token.
type File struct {
	Hostname             string `json:"hostname"`
	Port                 int    `json:"port"`
	LocalhostOnly        bool   `json:"localhost_only"`
	TunnelToken          string `json:"tunnel_token"`
	TunnelID             string `json:"tunnel_id,omitempty"`
	TunnelName           string `json:"tunnel_name,omitempty"`
	ServiceTokenClientID string `json:"service_token_client_id,omitempty"`
	ServiceTokenSecret   string `json:"service_token_secret,omitempty"`
	ZoneName             string `json:"zone_name,omitempty"`
}

// Find returns the sidecar path if one exists, looking next to the executable
// first (and next to the .app bundle on macOS) and then in the user config dir.
// ok is false when no file is found.
func Find() (string, bool) {
	var cands []string
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		cands = append(cands, exeCandidates(exe)...)
	}
	if dir, err := config.Dir(); err == nil {
		cands = append(cands, filepath.Join(dir, Name))
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// exeCandidates lists sidecar paths for an executable. Inside a macOS bundle the
// binary lives in X.app/Contents/MacOS, but customers put the sidecar beside X.app.
func exeCandidates(exe string) []string {
	dir := filepath.Dir(exe)
	cands := []string{filepath.Join(dir, Name)}
	contents := filepath.Dir(dir)
	bundle := filepath.Dir(contents)
	if filepath.Base(dir) == "MacOS" && filepath.Base(contents) == "Contents" && strings.HasSuffix(bundle, ".app") {
		cands = append(cands, filepath.Join(filepath.Dir(bundle), Name))
	}
	return cands
}

// Load reads and validates a sidecar file.
func Load(path string) (File, error) {
	var f File
	data, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, err
	}
	if err := f.Validate(); err != nil {
		return f, err
	}
	return f, nil
}

// Save writes the sidecar file with 0600 permissions (it holds a tunnel token).
func Save(path string, f File) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Validate checks the fields the customer runtime depends on.
func (f File) Validate() error {
	if f.TunnelToken == "" {
		return errors.New("预配文件缺少 tunnel_token")
	}
	if f.Hostname == "" {
		return errors.New("预配文件缺少 hostname")
	}
	if f.Port < 1 || f.Port > 65535 {
		return errors.New("预配文件里的端口无效")
	}
	return nil
}

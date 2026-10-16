package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Request is the file the GUI writes before starting the privileged helper.
// The helper owns both sshd and cloudflared so they stop if the window process dies.
type Request struct {
	ParentPID    int    `json:"parent_pid"`
	Port         int    `json:"port"`
	Listen       string `json:"listen"`
	AllowUser    string `json:"allow_user"`
	PasswordAuth bool   `json:"password_auth"`
	PermitRoot   bool   `json:"permit_root"`
	Cloudflared  string `json:"cloudflared"`
	TunnelToken  string `json:"tunnel_token"`
	Hostname     string `json:"hostname"`
	// UserName and NewPassword are set when the user chose to give the account
	// a new password. NewPassword is DPAPI-sealed to that user, never plain text,
	// and the helper strips it from this file as soon as it has read it.
	UserName    string `json:"user_name,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
}

type Status struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
	SSH    string `json:"ssh"`
	Tunnel string `json:"tunnel"`
	Port   int    `json:"port"`
}

func RequestPath(dir string) string { return filepath.Join(dir, "request.json") }
func StatusPath(dir string) string  { return filepath.Join(dir, "status.json") }
func LogPath(dir string) string     { return filepath.Join(dir, "helper.log") }
func ShutdownPath(dir string) string {
	return filepath.Join(dir, "shutdown")
}

func WriteRequest(dir string, req Request) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return err
	}
	path := RequestPath(dir)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return nil
}

func ReadStatus(dir string) (Status, error) {
	data, err := os.ReadFile(StatusPath(dir))
	if err != nil {
		return Status{}, err
	}
	var st Status
	err = json.Unmarshal(data, &st)
	return st, err
}

func writeStatus(dir string, st Status) {
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	path := StatusPath(dir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// File is the saved GUI state. The API token is a secret; the file mode is 0600.
type File struct {
	APIToken             string `json:"api_token"`
	AccountID            string `json:"account_id"`
	ZoneID               string `json:"zone_id"`
	ZoneName             string `json:"zone_name"`
	Subdomain            string `json:"subdomain"`
	Emails               string `json:"emails,omitempty"`
	ServiceTokenID       string `json:"service_token_id,omitempty"`
	ServiceTokenClientID string `json:"service_token_client_id,omitempty"`
	ServiceTokenSecret   string `json:"service_token_secret,omitempty"`
	Port                 int    `json:"port"`
	LocalhostOnly        bool   `json:"localhost_only"`
	PasswordAuth         bool   `json:"password_auth"`
	TunnelID             string `json:"tunnel_id"`
	TunnelName           string `json:"tunnel_name"`
	Hostname             string `json:"hostname"`
}

func Default() File {
	return File{
		Subdomain:     "ssh",
		Port:          2222,
		LocalhostOnly: true,
		PasswordAuth:  true,
		TunnelName:    "cfssh",
	}
}

func Dir() (string, error) {
	if runtime.GOOS == "windows" {
		root := os.Getenv("AppData")
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			root = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(root, "cfssh"), nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cfssh"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cfssh"), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func Load() (File, error) {
	cfg := Default()
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	if cfg.Port == 0 {
		cfg.Port = 2222
	}
	if cfg.Subdomain == "" {
		cfg.Subdomain = "ssh"
	}
	if cfg.TunnelName == "" {
		cfg.TunnelName = "cfssh"
	}
	return cfg, nil
}

func Save(cfg File) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(dir, "config.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var subdomainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func ValidSubdomain(s string) bool {
	return subdomainRE.MatchString(s)
}

func Hostname(subdomain, zone string) string {
	return strings.TrimSpace(subdomain) + "." + strings.TrimSpace(zone)
}

func ParseEmails(raw string) ([]string, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == ' ' || r == '\t'
	})
	var out []string
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if !strings.Contains(p, "@") || strings.HasPrefix(p, "@") || strings.HasSuffix(p, "@") {
			return nil, errors.New("邮箱格式不对: " + p)
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("至少填写一个允许登录的邮箱")
	}
	return out, nil
}

func ListenAddr(localhostOnly bool) string {
	if localhostOnly {
		return "127.0.0.1"
	}
	return "0.0.0.0"
}

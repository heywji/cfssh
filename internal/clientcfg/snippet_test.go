package clientcfg

import "strings"
import "testing"

func TestCommandAndConfig(t *testing.T) {
	cmd := Command("ada", "ssh.example.com", "id.access", "secret")
	if !strings.Contains(cmd, "ada@ssh.example.com") || !strings.Contains(cmd, "--service-token-id 'id.access'") {
		t.Fatal(cmd)
	}
	cfg := SSHConfig("ada", "ssh.example.com", "/home/ada/.ssh/cfssh_ed25519", "id.access", "secret")
	if !strings.Contains(cfg, "Host ssh.example.com") || !strings.Contains(cfg, "IdentityFile /home/ada/.ssh/cfssh_ed25519") || !strings.Contains(cfg, "--service-token-secret 'secret'") {
		t.Fatal(cfg)
	}
}

package clientcfg

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

func CurrentUser() string {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return "user"
	}
	name := u.Username
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	return name
}

func Command(userName, host, clientID, secret string) string {
	return fmt.Sprintf("ssh -o ProxyCommand=\"%s\" %s@%s", proxyCommand(clientID, secret), userName, host)
}

func SSHConfig(userName, host, keyPath, clientID, secret string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Host %s\n", host)
	fmt.Fprintf(&b, "    User %s\n", userName)
	fmt.Fprintf(&b, "    ProxyCommand %s\n", proxyCommand(clientID, secret))
	if keyPath != "" {
		fmt.Fprintf(&b, "    IdentityFile %s\n", keyPath)
	}
	return b.String()
}

func proxyCommand(clientID, secret string) string {
	return fmt.Sprintf("cloudflared access ssh --hostname %%h --service-token-id '%s' --service-token-secret '%s'", clientID, secret)
}

func KeyPaths() (privatePath, publicPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	privatePath = filepath.Join(home, ".ssh", "cfssh_ed25519")
	publicPath = privatePath + ".pub"
	return privatePath, publicPath, nil
}

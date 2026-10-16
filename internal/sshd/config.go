package sshd

import (
	"fmt"
	"regexp"
	"strings"
)

type UnixOptions struct {
	Port         int
	Listen       string
	HostKeys     []string
	SFTP         string
	AllowUser    string
	PasswordAuth bool
	PermitRoot   bool
}

func UnixConfig(o UnixOptions) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Port %d\n", o.Port)
	fmt.Fprintf(&b, "ListenAddress %s\n", o.Listen)
	fmt.Fprintf(&b, "AddressFamily inet\n")
	for _, key := range o.HostKeys {
		fmt.Fprintf(&b, "HostKey %s\n", key)
	}
	b.WriteString("PubkeyAuthentication yes\n")
	if o.PasswordAuth {
		b.WriteString("PasswordAuthentication yes\n")
		b.WriteString("KbdInteractiveAuthentication yes\n")
	} else {
		b.WriteString("PasswordAuthentication no\n")
		b.WriteString("KbdInteractiveAuthentication no\n")
	}
	b.WriteString("PermitEmptyPasswords no\n")
	if o.PermitRoot {
		if o.PasswordAuth {
			b.WriteString("PermitRootLogin yes\n")
		} else {
			b.WriteString("PermitRootLogin prohibit-password\n")
		}
	} else {
		b.WriteString("PermitRootLogin no\n")
	}
	b.WriteString("AuthorizedKeysFile .ssh/authorized_keys\n")
	b.WriteString("UsePAM yes\n")
	b.WriteString("UseDNS no\n")
	b.WriteString("PrintMotd no\n")
	b.WriteString("X11Forwarding no\n")
	b.WriteString("ClientAliveInterval 30\n")
	b.WriteString("ClientAliveCountMax 4\n")
	if o.AllowUser != "" {
		fmt.Fprintf(&b, "AllowUsers %s\n", o.AllowUser)
	}
	if o.SFTP != "" {
		fmt.Fprintf(&b, "Subsystem sftp %s\n", o.SFTP)
	}
	return b.String()
}

var directiveRE = regexp.MustCompile(`(?i)^\s*(port|listenaddress|passwordauthentication|pubkeyauthentication|kbdinteractiveauthentication|allowusers)\b`)

const (
	markerBegin = "# BEGIN cfssh managed"
	markerEnd   = "# END cfssh managed"
)

// PatchWindows rewrites the directives this app owns and leaves the rest of
// the system sshd_config in place. Callers keep a pristine backup.
func PatchWindows(original string, port int, listen, allowUser string, password bool) string {
	stripped := stripManaged(original)
	var kept []string
	for _, line := range strings.Split(stripped, "\n") {
		if directiveRE.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	body := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	pass := "yes"
	if !password {
		pass = "no"
	}
	var b strings.Builder
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString(markerBegin + "\n")
	fmt.Fprintf(&b, "Port %d\n", port)
	fmt.Fprintf(&b, "ListenAddress %s\n", listen)
	b.WriteString("PubkeyAuthentication yes\n")
	fmt.Fprintf(&b, "PasswordAuthentication %s\n", pass)
	if allowUser != "" {
		fmt.Fprintf(&b, "AllowUsers %s\n", allowUser)
	}
	b.WriteString(markerEnd + "\n")
	return b.String()
}

func stripManaged(original string) string {
	lines := strings.Split(original, "\n")
	var out []string
	skip := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == markerBegin {
			skip = true
			continue
		}
		if trim == markerEnd {
			skip = false
			continue
		}
		if !skip {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

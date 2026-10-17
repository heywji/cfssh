//go:build darwin

package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A stand-in cloudflared: it echoes its arguments and only "registers" when the
// token arrived through TUNNEL_TOKEN, like the real binary would fail without it.
const fakeCloudflared = `#!/bin/sh
echo "args: $*"
if [ "$TUNNEL_TOKEN" = "tok-123" ]; then
  echo "INF Registered tunnel connection connIndex=0"
else
  echo "ERR no token"
fi
sleep 30
`

func TestStartCloudflaredPassesTokenByEnv(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "cloudflared")
	if err := os.WriteFile(bin, []byte(fakeCloudflared), 0o755); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var lines []string
	logf := func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() }
	connected := make(chan struct{})
	var once sync.Once

	p, err := startCloudflared(dir, Request{Cloudflared: bin, TunnelToken: "tok-123"}, "http2", logf,
		func(line string) {
			if strings.Contains(line, "Registered tunnel connection") {
				once.Do(func() { close(connected) })
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	defer p.stop()

	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("tunnel never reported connected; log:\n%s", strings.Join(lines, "\n"))
	}
	if _, err := os.Stat(tokenPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("token must not be written to disk, stat err=%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	var args string
	for _, l := range lines {
		if strings.HasPrefix(l, "args: ") {
			args = l
		}
	}
	if !strings.HasSuffix(args, " --protocol http2 run") || strings.Contains(args, "--metrics") || strings.Contains(args, "--token") {
		t.Fatalf("unexpected cloudflared args: %q", args)
	}
}

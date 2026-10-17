package supervisor

import (
	"testing"
	"time"
)

func TestTunnelHealthDownOnlyWhenAllConnectionsDrop(t *testing.T) {
	now := time.Unix(1000, 0)
	h := newTunnelHealth()
	h.now = func() time.Time { return now }

	if h.downFor() != 0 {
		t.Fatal("a tunnel that never connected is not counted as down")
	}
	h.line("INF Registered tunnel connection connIndex=0 connection=a protocol=quic")
	h.line("INF Registered tunnel connection connIndex=1 connection=b protocol=quic")
	h.line(`ERR Serve tunnel error error="timeout: no recent network activity" connIndex=0`)
	if !h.connected() || h.downFor() != 0 {
		t.Fatal("one connection left must keep the tunnel up")
	}
	h.line(`ERR failed to serve tunnel connection error="x" connIndex=1`)
	if h.connected() {
		t.Fatal("no connection left, tunnel should be down")
	}
	now = now.Add(75 * time.Second)
	if got := h.downFor(); got != 75*time.Second {
		t.Fatalf("downFor = %v", got)
	}
	h.line("INF Registered tunnel connection connIndex=1 connection=c protocol=quic")
	if !h.connected() || h.downFor() != 0 {
		t.Fatal("re-registering must clear the down clock")
	}
}

func TestTunnelHealthIgnoresErrorsForUnknownConnections(t *testing.T) {
	h := newTunnelHealth()
	h.line("INF Registered tunnel connection connIndex=0")
	h.line(`ERR Serve tunnel error error="TLS handshake with edge error: EOF" connIndex=2`)
	if !h.connected() {
		t.Fatal("an error for a connection that never came up must not drop the tunnel")
	}
}

func TestTunnelHealthWaitingCountsFromRestart(t *testing.T) {
	now := time.Unix(1000, 0)
	h := newTunnelHealth()
	h.now = func() time.Time { return now }
	h.waiting()
	now = now.Add(tunnelDownLimit)
	if h.downFor() < tunnelDownLimit {
		t.Fatal("a restart that never registers must still time out")
	}
}

func TestNextProtocolAlternates(t *testing.T) {
	if p := nextProtocol(""); p != "http2" {
		t.Fatalf("after the default run: %q", p)
	}
	if p := nextProtocol("http2"); p != "quic" {
		t.Fatalf("after http2: %q", p)
	}
	if p := nextProtocol("quic"); p != "http2" {
		t.Fatalf("after quic: %q", p)
	}
}

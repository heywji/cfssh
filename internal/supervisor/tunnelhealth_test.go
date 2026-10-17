package supervisor

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// The 2026-10-17 drop on the Windows machine, as cloudflared logged it: QUIC
// timed out on every connection and cloudflared never came back by itself.
func TestTunnelHealthCatchesRealQUICTimeout(t *testing.T) {
	now := time.Unix(1000, 0)
	h := newTunnelHealth()
	h.now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		h.line("INF Registered tunnel connection connIndex=" + string(rune('0'+i)) + " connection=x event=0 ip=198.41.200.23 location=lax10 protocol=quic")
	}
	for _, l := range []string{
		`ERR Failed to dial a quic connection error="failed to dial to edge with quic: timeout: no recent network activity" connIndex=2 event=0 ip=198.41.192.37`,
		`ERR Serve tunnel error error="accept stream listener error: failed to accept QUIC stream: timeout: no recent network activity" connIndex=0 event=0 ip=198.41.200.23`,
		`INF Retrying connection in up to 32s connIndex=0 event=0 ip=198.41.200.23`,
		`ERR Connection terminated error="failed to dial to edge with quic: timeout: no recent network activity" connIndex=1`,
		`ERR Connection terminated error="failed to dial to edge with quic: timeout: no recent network activity" connIndex=2`,
		`ERR Connection terminated error="failed to dial to edge with quic: timeout: no recent network activity" connIndex=3`,
	} {
		h.line(l)
	}
	now = now.Add(tunnelDownLimit)
	if h.downFor() < tunnelDownLimit {
		t.Fatalf("downFor = %v, the helper would not restart", h.downFor())
	}
}

func TestTunnelHealthReadyOverridesLog(t *testing.T) {
	now := time.Unix(1000, 0)
	h := newTunnelHealth()
	h.now = func() time.Time { return now }
	h.line("INF Registered tunnel connection connIndex=0")
	// The connection went away with a log line no marker matches.
	h.setReady(0)
	if h.connected() {
		t.Fatal("/ready says 0 connections, tunnel must be down")
	}
	now = now.Add(tunnelDownLimit)
	h.setReady(0)
	if h.downFor() < tunnelDownLimit {
		t.Fatalf("downFor = %v", h.downFor())
	}
	h.setReady(2)
	if !h.connected() || h.downFor() != 0 {
		t.Fatal("/ready back above 0 must clear the down clock")
	}
}

func TestTunnelHealthStaleReadyFallsBackToLog(t *testing.T) {
	now := time.Unix(1000, 0)
	h := newTunnelHealth()
	h.now = func() time.Time { return now }
	h.line("INF Registered tunnel connection connIndex=0")
	h.setReady(0)
	now = now.Add(readyFresh)
	if !h.connected() || h.downFor() != 0 {
		t.Fatal("an old /ready answer must not keep the tunnel down")
	}
}

func TestTunnelHealthReadsMetricsAddr(t *testing.T) {
	h := newTunnelHealth()
	h.line("2026-10-17T11:43:11Z INF Starting metrics server on 127.0.0.1:20241/metrics")
	if got := h.metricsAddr(); got != "127.0.0.1:20241" {
		t.Fatalf("metricsAddr = %q", got)
	}
}

func TestFetchReady(t *testing.T) {
	code, body := 200, `{"status":200,"readyConnections":4,"connectorId":"x"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	if n, ok := fetchReady(srv.Client(), addr); !ok || n != 4 {
		t.Fatalf("200: n=%d ok=%v", n, ok)
	}
	code, body = 503, `{"status":503,"readyConnections":0,"connectorId":"x"}`
	if n, ok := fetchReady(srv.Client(), addr); !ok || n != 0 {
		t.Fatalf("503: n=%d ok=%v", n, ok)
	}
	body = "not json"
	if _, ok := fetchReady(srv.Client(), addr); ok {
		t.Fatal("a body without readyConnections must not count")
	}
}

func TestRestartGapBacksOff(t *testing.T) {
	want := []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, 5 * time.Minute, 5 * time.Minute}
	for fails, w := range want {
		if got := restartGap(fails); got != w {
			t.Fatalf("restartGap(%d) = %v, want %v", fails, got, w)
		}
	}
}

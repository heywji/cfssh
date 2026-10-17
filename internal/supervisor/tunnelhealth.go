package supervisor

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// tunnelDownLimit is how long every tunnel connection may stay down before the
// helper restarts cloudflared. cloudflared reconnects on its own, but after a
// QUIC timeout it has been seen to stay offline for minutes.
const tunnelDownLimit = 60 * time.Second

// tunnelRestartGap keeps a cloudflared that exits right away from being
// restarted in a tight loop. Each restart that does not bring the tunnel back
// doubles the gap, up to tunnelRestartMaxGap.
const (
	tunnelRestartGap    = 10 * time.Second
	tunnelRestartMaxGap = 5 * time.Minute
)

// restartGap is the wait before the next restart after fails restarts in a
// row that never got a connection registered.
func restartGap(fails int) time.Duration {
	gap := tunnelRestartGap
	for i := 0; i < fails && gap < tunnelRestartMaxGap; i++ {
		gap *= 2
	}
	if gap > tunnelRestartMaxGap {
		gap = tunnelRestartMaxGap
	}
	return gap
}

// tunnelStableFor is how long a connection must stay up before the down clock
// is cleared. On a network where QUIC flaps, a connection registers every ~40s
// and dies within seconds; counting each of those as a recovery kept the clock
// from ever reaching tunnelDownLimit, so the helper never tried HTTP/2.
const tunnelStableFor = 30 * time.Second

// readyFresh is how long a /ready answer is trusted. Past that, for example
// when the metrics server cannot be reached, the log tracker decides.
const readyFresh = 15 * time.Second

// tunnelHealth knows whether any connection to Cloudflare is registered.
// cloudflared keeps several connections (connIndex 0-3); the tunnel works while
// at least one of them is up. cloudflared's own /ready count is the main
// source; the log is the fallback, since a dropped connection can be logged in
// a way none of tunnelDownMarkers matches and would then look up forever.
type tunnelHealth struct {
	mu        sync.Mutex
	up        map[string]bool
	ever      bool
	downSince time.Time
	upSince   time.Time
	metrics   string // host:port of cloudflared's metrics server, from its log
	ready     int
	readyAt   time.Time
	now       func() time.Time
}

func newTunnelHealth() *tunnelHealth {
	return &tunnelHealth{up: map[string]bool{}, now: time.Now}
}

// Log lines that mean one connection went away. cloudflared logs a fresh
// "Registered tunnel connection" for the same connIndex once it is back.
var tunnelDownMarkers = []string{
	"Unregistered tunnel connection",
	"Serve tunnel error",
	"failed to serve tunnel connection",
	"Connection terminated",
}

const metricsMarker = "Starting metrics server on "

// waiting starts the down clock now, for a restarted cloudflared that has not
// registered yet: if it never does, downFor still grows and triggers a retry.
func (h *tunnelHealth) waiting() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ever = true
	h.downSince = h.now()
}

func (h *tunnelHealth) line(s string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Settle before and after, so a connection that has been up long enough
	// counts as stable even if nothing polled downFor in between.
	h.settle()
	defer h.settle()
	if i := strings.Index(s, metricsMarker); i >= 0 {
		addr := strings.Fields(s[i+len(metricsMarker):])
		if len(addr) > 0 {
			h.metrics = strings.TrimSuffix(addr[0], "/metrics")
		}
		return
	}
	idx := connIndex(s)
	if strings.Contains(s, "Registered tunnel connection") && !strings.Contains(s, "Unregistered") {
		h.up[idx] = true
		// This line is newer than the last /ready answer, which may still say
		// 0 from before the connection came up.
		h.readyAt = time.Time{}
		return
	}
	for _, m := range tunnelDownMarkers {
		if strings.Contains(s, m) {
			if h.up[idx] {
				delete(h.up, idx)
				h.readyAt = time.Time{}
			}
			return
		}
	}
}

// setReady records cloudflared's own count of ready connections.
func (h *tunnelHealth) setReady(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ready = n
	h.readyAt = h.now()
	h.settle()
}

// isUp and settle expect h.mu held.
func (h *tunnelHealth) isUp() bool {
	if !h.readyAt.IsZero() && h.now().Sub(h.readyAt) < readyFresh {
		return h.ready > 0
	}
	return len(h.up) > 0
}

func (h *tunnelHealth) settle() {
	now := h.now()
	if h.isUp() {
		h.ever = true
		if h.upSince.IsZero() {
			h.upSince = now
		}
		if now.Sub(h.upSince) >= tunnelStableFor {
			h.downSince = time.Time{}
		}
		return
	}
	h.upSince = time.Time{}
	if h.ever && h.downSince.IsZero() {
		h.downSince = now
	}
}

// connected reports whether at least one connection is registered.
func (h *tunnelHealth) connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.isUp()
}

// downFor is how long the tunnel has been without a stable connection, counted
// from when it was last stable. It is 0 while a connection is up, so a restart
// never cuts off one that is still settling; brief connections in between do
// not reset the clock.
func (h *tunnelHealth) downFor() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	// A stale /ready answer hands the decision back to the log tracker.
	h.settle()
	if !h.ever || h.downSince.IsZero() || h.isUp() {
		return 0
	}
	return h.now().Sub(h.downSince)
}

func (h *tunnelHealth) metricsAddr() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.metrics
}

// watchReady polls cloudflared's /ready until stop is closed.
func (h *tunnelHealth) watchReady(stop <-chan struct{}) {
	client := &http.Client{Timeout: 3 * time.Second}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		addr := h.metricsAddr()
		if addr == "" {
			continue
		}
		if n, ok := fetchReady(client, addr); ok {
			h.setReady(n)
		}
	}
}

// fetchReady reads readyConnections from cloudflared's /ready. It answers 200
// with connections and 503 without; both carry the JSON body.
func fetchReady(client *http.Client, addr string) (int, bool) {
	resp, err := client.Get("http://" + addr + "/ready")
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	var body struct {
		ReadyConnections *int `json:"readyConnections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.ReadyConnections == nil {
		return 0, false
	}
	return *body.ReadyConnections, true
}

func connIndex(s string) string {
	i := strings.Index(s, "connIndex=")
	if i < 0 {
		return ""
	}
	rest := s[i+len("connIndex="):]
	if j := strings.IndexAny(rest, " \t"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// nextProtocol picks the transport for a cloudflared restart. The first run uses
// cloudflared's default (QUIC, falling back on its own); after a drop the helper
// alternates, because some networks only keep HTTP/2 up and others only QUIC.
func nextProtocol(current string) string {
	if current == "http2" {
		return "quic"
	}
	return "http2"
}

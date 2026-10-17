package supervisor

import (
	"strings"
	"sync"
	"time"
)

// tunnelDownLimit is how long every tunnel connection may stay down before the
// helper restarts cloudflared. cloudflared reconnects on its own, but after a
// QUIC timeout it has been seen to stay offline for minutes.
const tunnelDownLimit = 60 * time.Second

// tunnelRestartGap keeps a cloudflared that exits right away from being
// restarted in a tight loop.
const tunnelRestartGap = 10 * time.Second

// tunnelHealth follows cloudflared's log to know whether any connection to
// Cloudflare is registered. cloudflared keeps several connections (connIndex
// 0-3); the tunnel works while at least one of them is up.
type tunnelHealth struct {
	mu        sync.Mutex
	up        map[string]bool
	ever      bool
	downSince time.Time
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

// waiting starts the down clock now, for a restarted cloudflared that has not
// registered yet: if it never does, downFor still grows and triggers a retry.
func (h *tunnelHealth) waiting() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ever = true
	h.downSince = h.now()
}

func (h *tunnelHealth) line(s string) {
	idx := connIndex(s)
	h.mu.Lock()
	defer h.mu.Unlock()
	if strings.Contains(s, "Registered tunnel connection") && !strings.Contains(s, "Unregistered") {
		h.up[idx] = true
		h.ever = true
		h.downSince = time.Time{}
		return
	}
	for _, m := range tunnelDownMarkers {
		if strings.Contains(s, m) {
			if !h.up[idx] {
				return
			}
			delete(h.up, idx)
			if len(h.up) == 0 {
				h.downSince = h.now()
			}
			return
		}
	}
}

// connected reports whether at least one connection is registered.
func (h *tunnelHealth) connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.up) > 0
}

// downFor is how long the tunnel has had no connection after having had one.
func (h *tunnelHealth) downFor() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.ever || len(h.up) > 0 || h.downSince.IsZero() {
		return 0
	}
	return h.now().Sub(h.downSince)
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

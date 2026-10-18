package main

import (
	"net"
	"sync"
	"time"
)

// Prober checks backend readiness with TCP dials. Results are cached for a
// short time so a burst of waiting requests does not hammer the backends.
type Prober struct {
	ttl  time.Duration
	mu   sync.Mutex
	last map[string]probeResult
}

type probeResult struct {
	ok bool
	at time.Time
}

// NewProber builds a prober whose cached results live for ttl.
func NewProber(ttl time.Duration) *Prober {
	return &Prober{ttl: ttl, last: make(map[string]probeResult)}
}

// Ready reports whether the upstream accepts TCP connections.
func (p *Prober) Ready(upstream string) bool {
	p.mu.Lock()
	res, cached := p.last[upstream]
	p.mu.Unlock()
	if cached && time.Since(res.at) < p.ttl {
		return res.ok
	}

	ok := false
	if conn, err := net.DialTimeout("tcp", upstream, 750*time.Millisecond); err == nil {
		conn.Close()
		ok = true
	}

	p.mu.Lock()
	p.last[upstream] = probeResult{ok: ok, at: time.Now()}
	p.mu.Unlock()
	return ok
}

package main

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed loading.html
var loadingHTML []byte

// httpRoute is one public HTTP listener: what it proxies to and which
// upstream must be ready for a given request path.
type httpRoute struct {
	port     int
	target   string
	probeFor func(path string) string
}

// Gatekeeper fronts the sleeping devbox container. It wakes the container on
// traffic, serves a loading page while the requested backend starts, proxies
// the request once the backend is ready and stops the container again after
// a period without activity.
type Gatekeeper struct {
	cfg    *Config
	docker *DockerClient
	prober *Prober

	pxMu    sync.Mutex
	proxies map[string]*httputil.ReverseProxy

	stateMu      sync.Mutex
	stateRunning bool
	stateAt      time.Time

	tunnels      atomic.Int64 // open websocket and SSH sessions keep the app awake
	lastActivity atomic.Int64 // unix seconds of the last proxied request
}

// NewGatekeeper builds the daemon and prepares a reverse proxy per upstream.
func NewGatekeeper(cfg *Config, docker *DockerClient) (*Gatekeeper, error) {
	g := &Gatekeeper{
		cfg:     cfg,
		docker:  docker,
		prober:  NewProber(time.Second),
		proxies: make(map[string]*httputil.ReverseProxy),
	}
	for _, target := range []string{
		cfg.HTTPUpstream, cfg.SSHUpstream, cfg.CodeUpstream,
		cfg.OpenUpstream, cfg.TtydUpstream, cfg.PiUpstream,
	} {
		if _, err := url.Parse("http://" + target); err != nil {
			return nil, err
		}
		g.proxies[target] = g.newProxy(target)
	}
	return g, nil
}

func (g *Gatekeeper) newProxy(target string) *httputil.ReverseProxy {
	px := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: target})
	px.FlushInterval = 100 * time.Millisecond
	px.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("proxy %s %s -> %s: %v", r.Method, r.URL.Path, target, err)
		g.serveLoading(w)
	}
	return px
}

// HandleHTTP returns the handler for one public HTTP port.
func (g *Gatekeeper) HandleHTTP(route *httpRoute) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authenticate before anything wakes the app or counts as activity.
		if a := g.cfg.Auth; a != nil && !a.Check(r) {
			a.Challenge(w)
			return
		}
		g.ensureRunning()
		g.touch()

		if !g.prober.Ready(route.probeFor(r.URL.Path)) {
			g.serveLoading(w)
			return
		}

		if r.Header.Get("Upgrade") != "" {
			// ReverseProxy blocks until the tunnel closes, so an open
			// websocket counts as activity while it lives.
			g.tunnels.Add(1)
			defer g.tunnels.Add(-1)
		}
		g.proxyFor(route.target).ServeHTTP(w, r)
	})
}

func (g *Gatekeeper) proxyFor(target string) *httputil.ReverseProxy {
	g.pxMu.Lock()
	defer g.pxMu.Unlock()
	return g.proxies[target]
}

// serveLoading answers with the generic loading page. The page polls the
// same URL and reloads as soon as a response is not marked as starting.
func (g *Gatekeeper) serveLoading(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Retry-After", "2")
	h.Set("X-Gatekeeper-State", "starting")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write(loadingHTML)
}

// ensureRunning starts the devbox container when it is not running. Docker
// state is cached for a few seconds, so bursts of requests do not hit the
// API.
func (g *Gatekeeper) ensureRunning() {
	g.stateMu.Lock()
	cached := time.Since(g.stateAt) < 5*time.Second
	running := g.stateRunning
	g.stateMu.Unlock()
	if cached && running {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	running, err := g.docker.Running(ctx, g.cfg.AppContainer)
	if err != nil {
		log.Printf("inspect %s: %v", g.cfg.AppContainer, err)
		return
	}
	if !running {
		if err := g.docker.Start(ctx, g.cfg.AppContainer); err != nil {
			log.Printf("start %s: %v", g.cfg.AppContainer, err)
			return
		}
		log.Printf("started %s", g.cfg.AppContainer)
		running = true
	}

	g.stateMu.Lock()
	g.stateRunning = running
	g.stateAt = time.Now()
	g.stateMu.Unlock()
}

// isRunning reports whether the app container runs, without starting it.
// The answer is cached for a few seconds like ensureRunning's.
func (g *Gatekeeper) isRunning() bool {
	g.stateMu.Lock()
	if time.Since(g.stateAt) < 5*time.Second {
		running := g.stateRunning
		g.stateMu.Unlock()
		return running
	}
	g.stateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	running, err := g.docker.Running(ctx, g.cfg.AppContainer)
	if err != nil {
		log.Printf("inspect %s: %v", g.cfg.AppContainer, err)
		return false
	}
	g.stateMu.Lock()
	g.stateRunning = running
	g.stateAt = time.Now()
	g.stateMu.Unlock()
	return running
}

// markStopped records that the app is no longer running, so the idle loop
// does not try to stop it again.
func (g *Gatekeeper) markStopped() {
	g.stateMu.Lock()
	g.stateRunning = false
	g.stateAt = time.Now()
	g.stateMu.Unlock()
}

func (g *Gatekeeper) touch() {
	g.lastActivity.Store(time.Now().Unix())
}

// IdleLoop stops the devbox container after IdleTimeout without proxied
// traffic and without open tunnels. Websocket and SSH sessions count as open
// tunnels and hold the container awake.
func (g *Gatekeeper) IdleLoop() {
	if g.cfg.IdleTimeout <= 0 {
		log.Printf("idle stop disabled")
		return
	}
	log.Printf("idle stop: will stop %s after %s without activity", g.cfg.AppContainer, g.cfg.IdleTimeout)
	for {
		time.Sleep(10 * time.Second)
		if g.tunnels.Load() > 0 {
			continue
		}
		idle := time.Since(time.Unix(g.lastActivity.Load(), 0))
		if idle < g.cfg.IdleTimeout {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		running, err := g.docker.Running(ctx, g.cfg.AppContainer)
		if err != nil {
			log.Printf("inspect %s: %v", g.cfg.AppContainer, err)
			cancel()
			continue
		}
		if !running {
			g.markStopped()
			g.touch()
			cancel()
			continue
		}
		log.Printf("idle for %s, stopping %s", idle.Round(time.Second), g.cfg.AppContainer)
		if err := g.docker.Stop(ctx, g.cfg.AppContainer, 10); err != nil {
			log.Printf("stop %s: %v", g.cfg.AppContainer, err)
		} else {
			log.Printf("stopped %s", g.cfg.AppContainer)
			g.markStopped()
			g.touch()
		}
		cancel()
	}
}

// Shutdown stops the devbox container when the gatekeeper itself goes away.
// The devbox container runs inside the network namespace of the gatekeeper,
// so it must not outlive it.
func (g *Gatekeeper) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := g.docker.Stop(ctx, g.cfg.AppContainer, 10); err != nil {
		log.Printf("stop %s on shutdown: %v", g.cfg.AppContainer, err)
		return
	}
	log.Printf("stopped %s on shutdown", g.cfg.AppContainer)
}

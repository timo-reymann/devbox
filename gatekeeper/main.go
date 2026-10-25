// Command gatekeeper fronts the sleeping devbox container. It listens on the
// public ports, wakes the devbox container on any connection, serves a
// generic loading page until the requested backend is ready, proxies traffic
// once it is, and stops the devbox container again after idle.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Public listen ports. docker-compose.yml publishes exactly these on the
// gatekeeper container.
const (
	portWeb = 8080 // front door: dashboard and all Caddy routes
	portSSH = 8022 // sshd, raw TCP
)

func main() {
	log.SetPrefix("gatekeeper: ")
	log.SetFlags(log.LstdFlags)

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	g, err := NewGatekeeper(cfg, NewDockerClient(cfg.DockerSocket))
	if err != nil {
		log.Fatalf("%v", err)
	}
	g.touch() // the idle clock starts with the gatekeeper

	if cfg.Auth == nil {
		log.Printf("WARNING: BASIC_AUTH_PASSWORD/BASIC_AUTH_HASH not set, any traffic wakes %s", cfg.AppContainer)
	}
	log.Printf("waking %s on traffic, idle stop %s", cfg.AppContainer, idleOrOff(cfg.IdleTimeout))
	log.Printf("upstreams: web=%s ssh=%s code=%s opencode=%s ttyd=%s pi=%s",
		cfg.HTTPUpstream, cfg.SSHUpstream, cfg.CodeUpstream, cfg.OpenUpstream, cfg.TtydUpstream, cfg.PiUpstream)

	// Sleep the devbox container again when it goes unused.
	go g.IdleLoop()

	// Raw TCP proxy for sshd.
	go func() {
		if err := g.ServeTCP(portSSH, cfg.SSHUpstream); err != nil {
			log.Fatalf("tcp :%d: %v", portSSH, err)
		}
	}()

	// HTTP proxies. Everything on portWeb goes to the app Caddy; the
	// per-path probes decide when a route stops showing the loading page.
	routes := []httpRoute{
		{port: portWeb, target: cfg.HTTPUpstream, probeFor: cfg.frontProbe},
	}
	for i := range routes {
		route := routes[i]
		go func() {
			srv := &http.Server{Addr: fmt.Sprintf(":%d", route.port), Handler: g.HandleHTTP(&route)}
			log.Printf("listening on :%d (http -> %s)", route.port, route.target)
			if err := srv.ListenAndServe(); err != http.ErrServerClosed && err != nil {
				log.Fatalf("http :%d: %v", route.port, err)
			}
		}()
	}

	// When the gatekeeper stops, the devbox container has to stop too: it
	// runs inside the gatekeeper's network namespace.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	s := <-sig
	log.Printf("%s received, stopping %s", s, cfg.AppContainer)
	g.Shutdown()
}

func idleOrOff(d time.Duration) string {
	if d <= 0 {
		return "off"
	}
	return d.String()
}

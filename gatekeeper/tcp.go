package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

// ServeTCP listens for raw TCP connections and splices them to target once
// it is ready. Connections that arrive while the devbox container starts are
// held open, so an SSH client just sees a longer connect time.
func (g *Gatekeeper) ServeTCP(port int, target string) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	log.Printf("listening on :%d (tcp -> %s)", port, target)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go g.splice(conn, target)
	}
}

func (g *Gatekeeper) splice(conn net.Conn, target string) {
	defer conn.Close()

	if g.cfg.Auth != nil {
		// A raw TCP connection cannot be authenticated here, so it must
		// not wake the app or keep it awake: only a login on the web side
		// starts the container.
		if !g.isRunning() {
			log.Printf("refusing %s: %s is asleep, log in on the web UI to wake it", conn.RemoteAddr(), g.cfg.AppContainer)
			return
		}
		closed := make(chan struct{})
		defer close(closed)
		go g.holdAfterSettle(closed)
	} else {
		// The held connection is an open session: it keeps the app awake
		// while it waits and while it is spliced.
		g.tunnels.Add(1)
		defer g.tunnels.Add(-1)
		g.ensureRunning()
		g.touch()
	}

	if !g.awaitReady(target) {
		return
	}

	up, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		log.Printf("dial %s: %v", target, err)
		return
	}
	defer up.Close()

	done := make(chan struct{})
	go func() { _, _ = io.Copy(up, conn); close(done) }()
	go func() { _, _ = io.Copy(conn, up); close(done) }()
	<-done
}

// sshSettle must exceed LoginGraceTime in ssh/sshd_config.d/10-devbox.conf,
// so a connection that stays open this long has authenticated.
const sshSettle = 30 * time.Second

// holdAfterSettle counts a connection as an open session only once it
// outlived sshSettle, so connections that never log in cannot keep the app
// awake.
func (g *Gatekeeper) holdAfterSettle(closed <-chan struct{}) {
	select {
	case <-time.After(sshSettle):
	case <-closed:
		return
	}
	g.tunnels.Add(1)
	g.touch()
	<-closed
	g.tunnels.Add(-1)
	g.touch()
}

// awaitReady waits until the probe passes or the start timeout expires.
func (g *Gatekeeper) awaitReady(target string) bool {
	deadline := time.Now().Add(g.cfg.StartTimeout)
	for {
		if g.prober.Ready(target) {
			return true
		}
		if time.Now().After(deadline) {
			log.Printf("%s not ready after %s, closing held connection", target, g.cfg.StartTimeout)
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

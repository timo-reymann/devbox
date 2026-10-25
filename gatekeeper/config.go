package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds everything the gatekeeper needs. The upstream ports come from
// the same environment variables that the devbox entrypoints read, so one
// environment block can drive both containers that share a network
// namespace. The built-in defaults match the layout in docker-compose.yml.
type Config struct {
	AppContainer string        // devbox container to start and stop
	IdleTimeout  time.Duration // stop the app after this much inactivity; 0 disables
	StartTimeout time.Duration // give up on connections held during startup
	DockerSocket string        // Docker control socket
	Auth         *Auth         // basic auth checked before waking the app; nil when unset

	// Upstreams inside the shared network namespace.
	HTTPUpstream string // app Caddy: dashboard and reverse proxy
	SSHUpstream  string // sshd
	CodeUpstream string // code-server
	OpenUpstream string // OpenCode server
	TtydUpstream string // ttyd web terminal
	PiUpstream   string // PI WEB
	PiPath       string // URL prefix PI WEB is served under, e.g. /pi
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// LoadConfig reads the configuration from the environment.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		AppContainer: env("APP_CONTAINER", "devbox-app"),
		DockerSocket: env("GATEKEEPER_DOCKER_SOCKET", "/var/run/docker.sock"),
		HTTPUpstream: "127.0.0.1:" + env("HTTP_PORT", "18080"),
		SSHUpstream:  "127.0.0.1:" + env("SSH_PORT", "18022"),
		CodeUpstream: "127.0.0.1:" + env("CODE_SERVER_PORT", "18082"),
		OpenUpstream: "127.0.0.1:" + env("OPENCODE_SERVER_PORT", "18081"),
		TtydUpstream: "127.0.0.1:" + env("TTYD_PORT", "18083"),
		PiUpstream:   "127.0.0.1:" + env("PI_WEB_PORT", "18504"),
		PiPath:       env("PI_WEB_PATH", "/pi"),
	}

	cfg.Auth = NewAuth(env("BASIC_AUTH_USER", "devbox"), os.Getenv("BASIC_AUTH_PASSWORD"), os.Getenv("BASIC_AUTH_HASH"))

	idle, err := parseDuration("IDLE_TIMEOUT", env("IDLE_TIMEOUT", "30m"))
	if err != nil {
		return nil, err
	}
	cfg.IdleTimeout = idle

	start, err := parseDuration("START_TIMEOUT", env("START_TIMEOUT", "2m"))
	if err != nil {
		return nil, err
	}
	cfg.StartTimeout = start

	if cfg.PiPath == "" || cfg.PiPath[0] != '/' {
		return nil, fmt.Errorf("PI_WEB_PATH %q must start with /", cfg.PiPath)
	}
	return cfg, nil
}

func parseDuration(name, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", name, value, err)
	}
	return d, nil
}

// frontProbe maps a front-door path to the upstream that must be ready
// before the request goes through. The mapping mirrors caddy/Caddyfile.
func (c *Config) frontProbe(path string) string {
	switch {
	case path == "/vscode" || hasSegment(path, "/vscode"):
		return c.CodeUpstream
	case path == "/terminal" || hasSegment(path, "/terminal"):
		return c.TtydUpstream
	case path == "/opencode" || hasSegment(path, "/opencode"):
		return c.OpenUpstream
	case path == c.PiPath || hasSegment(path, c.PiPath):
		return c.PiUpstream
	case path == "/":
		return c.HTTPUpstream
	default:
		// The Caddyfile catch-all proxies to the OpenCode server, and
		// /<base64url(dir)>/ entries land here too.
		return c.OpenUpstream
	}
}

// hasSegment reports whether path equals prefix or continues it at a segment
// boundary, so /pi does not match /pizza.
func hasSegment(path, prefix string) bool {
	if path == prefix {
		return true
	}
	return len(path) > len(prefix) && strings.HasPrefix(path, prefix+"/")
}

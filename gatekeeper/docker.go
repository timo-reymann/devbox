package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DockerClient controls exactly one container through the Docker API on a
// unix socket: inspect, start, stop. Nothing else.
type DockerClient struct {
	socket string
	http   *http.Client
}

// NewDockerClient builds a client that talks to the Docker API over the
// given unix socket.
func NewDockerClient(socket string) *DockerClient {
	return &DockerClient{
		socket: socket,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socket)
				},
			},
		},
	}
}

type dockerContainer struct {
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
}

// Running reports whether the container exists and is running.
func (d *DockerClient) Running(ctx context.Context, name string) (bool, error) {
	var state dockerContainer
	err := d.do(ctx, http.MethodGet, "/containers/"+name+"/json", name, &state)
	if err != nil {
		return false, err
	}
	return state.State.Running, nil
}

// Start starts the container. Starting a running container is not an error.
func (d *DockerClient) Start(ctx context.Context, name string) error {
	return d.do(ctx, http.MethodPost, "/containers/"+name+"/start", name, nil)
}

// Stop stops the container after a grace period in seconds. Stopping an
// already stopped container is not an error.
func (d *DockerClient) Stop(ctx context.Context, name string, graceSeconds int) error {
	path := fmt.Sprintf("/containers/%s/stop?t=%d", name, graceSeconds)
	return d.do(ctx, http.MethodPost, path, name, nil)
}

func (d *DockerClient) do(ctx context.Context, method, path, name string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker socket %s: %w", d.socket, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotModified:
		if out == nil {
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(out)
	case http.StatusNotFound:
		return fmt.Errorf("container %q not found - create the stack with: docker compose up -d --no-start", name)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

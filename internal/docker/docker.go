package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// MountedPath represents a .claude/projects directory found in a running container.
type MountedPath struct {
	ContainerName string // e.g. "nanoclaw-agent-3"
	HostPath      string // e.g. "/var/lib/docker/volumes/.../workspace/group/.claude/projects"
}

// Client talks to the Docker daemon over its Unix socket.
type Client struct {
	http *http.Client
}

// NewClient returns a Client that connects to socketPath (usually /var/run/docker.sock).
func NewClient(socketPath string) *Client {
	return &Client{
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
				},
			},
			Timeout: 10 * time.Second,
		},
	}
}

type container struct {
	Names  []string `json:"Names"`
	Mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
}

// FindClaudePaths returns all host-side .claude/projects paths from running containers.
func (c *Client) FindClaudePaths(ctx context.Context) ([]MountedPath, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/json?filters=%7B%22status%22%3A%5B%22running%22%5D%7D", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker socket: %w", err)
	}
	defer resp.Body.Close()

	var containers []container
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, err
	}

	var out []MountedPath
	for _, ct := range containers {
		name := "unknown"
		if len(ct.Names) > 0 {
			name = strings.TrimPrefix(ct.Names[0], "/")
		}
		for _, m := range ct.Mounts {
			// Match mounts ending in .claude/projects directly
			if strings.HasSuffix(m.Destination, ".claude/projects") {
				out = append(out, MountedPath{ContainerName: name, HostPath: m.Source})
			} else if strings.HasSuffix(m.Destination, ".claude") || strings.HasSuffix(m.Source, ".claude") {
				// Mount is to .claude — add the projects subdir on the host side
				hostProjects := m.Source + "/projects"
				out = append(out, MountedPath{ContainerName: name, HostPath: hostProjects})
			}
		}
	}
	return out, nil
}

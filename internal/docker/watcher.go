package docker

import (
	"context"
	"fmt"
	"time"
)

// PathEvent is emitted when a container's .claude/projects path appears or disappears.
type PathEvent struct {
	ContainerName string
	HostPath      string
	Added         bool // true = new path; false = path removed
}

// Watch polls Docker every interval and sends PathEvents on the returned channel.
// It sends Added=true for newly seen paths and Added=false for paths no longer present.
// The first poll sends Added=true for all current paths.
// Cancel ctx to stop.
func Watch(ctx context.Context, client *Client, interval time.Duration) (<-chan PathEvent, error) {
	// Initial poll to establish baseline
	initial, err := client.FindClaudePaths(ctx)
	if err != nil {
		return nil, fmt.Errorf("docker initial poll: %w", err)
	}

	ch := make(chan PathEvent, 16)
	known := make(map[string]string) // hostPath -> containerName

	go func() {
		defer close(ch)

		// Emit initial paths
		for _, p := range initial {
			known[p.HostPath] = p.ContainerName
			select {
			case ch <- PathEvent{ContainerName: p.ContainerName, HostPath: p.HostPath, Added: true}:
			case <-ctx.Done():
				return
			}
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, err := client.FindClaudePaths(ctx)
				if err != nil {
					// Log but continue — Docker may be briefly unavailable
					fmt.Printf("docker poll error: %v\n", err)
					continue
				}

				currentSet := make(map[string]string)
				for _, p := range current {
					currentSet[p.HostPath] = p.ContainerName
				}

				// Emit added paths
				for path, name := range currentSet {
					if _, exists := known[path]; !exists {
						known[path] = name
						select {
						case ch <- PathEvent{ContainerName: name, HostPath: path, Added: true}:
						case <-ctx.Done():
							return
						}
					}
				}

				// Emit removed paths
				for path, name := range known {
					if _, exists := currentSet[path]; !exists {
						delete(known, path)
						select {
						case ch <- PathEvent{ContainerName: name, HostPath: path, Added: false}:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()

	return ch, nil
}

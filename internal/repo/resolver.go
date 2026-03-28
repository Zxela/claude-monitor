package repo

import (
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const gitTimeout = 2 * time.Second

// Resolver resolves git repository identity from a working directory path.
type Resolver struct {
	mu    sync.RWMutex
	cache map[string]*Repo
}

// NewResolver creates a new Resolver with an empty cache.
func NewResolver() *Resolver {
	return &Resolver{
		cache: make(map[string]*Repo),
	}
}

// Resolve returns the Repo for the given cwd. Results are cached in memory.
// label is used as a container identifier when git is unavailable.
func (r *Resolver) Resolve(cwd, label string) (*Repo, error) {
	r.mu.RLock()
	if cached, ok := r.cache[cwd]; ok {
		r.mu.RUnlock()
		return cached, nil
	}
	r.mu.RUnlock()

	repo := r.resolve(cwd, label)

	r.mu.Lock()
	r.cache[cwd] = repo
	r.mu.Unlock()

	return repo, nil
}

func (r *Resolver) resolve(cwd, label string) *Repo {
	// Try git remote origin
	if rawURL, err := gitRemoteURL(cwd); err == nil && rawURL != "" {
		id, name, fullURL := normalizeRemoteURL(rawURL)
		return &Repo{ID: id, Name: name, URL: fullURL}
	}

	// Fallback: git toplevel basename
	if toplevel, err := gitToplevel(cwd); err == nil && toplevel != "" {
		base := filepath.Base(toplevel)
		return &Repo{ID: base, Name: base}
	}

	// Container fallback: label + basename
	base := filepath.Base(cwd)
	if label != "" {
		return &Repo{ID: label + "/" + base, Name: base}
	}

	// Final fallback: basename only
	return &Repo{ID: base, Name: base}
}

func gitRemoteURL(cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", cwd, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitToplevel(cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// normalizeRemoteURL parses a git remote URL and returns a normalized ID,
// human-readable name, and the original URL.
func normalizeRemoteURL(rawURL string) (id, name, fullURL string) {
	fullURL = strings.TrimSpace(rawURL)

	// Handle SCP-style: git@github.com:Zxela/claude-monitor.git
	if strings.Contains(fullURL, ":") && !strings.Contains(fullURL, "://") {
		parts := strings.SplitN(fullURL, ":", 2)
		host := parts[0]
		// Strip user@ prefix
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		path := strings.TrimSuffix(parts[1], ".git")
		path = strings.TrimPrefix(path, "/")
		id = host + "/" + path
		name = filepath.Base(path)
		return
	}

	// Handle standard URLs: https://, ssh://, git://
	parsed, err := url.Parse(fullURL)
	if err != nil {
		// Unparseable — use as-is
		stripped := strings.TrimSuffix(fullURL, ".git")
		name = filepath.Base(stripped)
		id = stripped
		return
	}

	host := parsed.Hostname()
	path := strings.TrimPrefix(parsed.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	id = host + "/" + path
	name = filepath.Base(path)
	return
}

// LoadCache populates the in-memory cache from persisted cwd→repoID mappings.
// Loaded entries have only the ID set; Name and URL are expected to come from
// the repos table.
func (r *Resolver) LoadCache(entries map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for cwd, repoID := range entries {
		r.cache[cwd] = &Repo{ID: repoID}
	}
}

// ClearCache removes all entries from the in-memory cache.
func (r *Resolver) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]*Repo)
}

// Cache returns a copy of the current in-memory cache.
func (r *Resolver) Cache() map[string]*Repo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]*Repo, len(r.cache))
	for k, v := range r.cache {
		copied := *v
		out[k] = &copied
	}
	return out
}

package repo

import (
	"net/url"
	"path/filepath"
	"strings"
	"sync"
)

const maxCacheSize = 1000

// Resolver resolves git repository identity from a working directory path.
type Resolver struct {
	git   Git
	mu    sync.RWMutex
	cache map[string]*Repo
}

// NewResolver creates a Resolver with an empty cache that asks git about cwds.
func NewResolver(git Git) *Resolver {
	return &Resolver{
		git:   git,
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

	resolved := r.resolve(cwd, label)

	// Re-check under write lock to avoid duplicate git spawns
	// from concurrent Resolve calls for the same cwd.
	r.mu.Lock()
	if cached, ok := r.cache[cwd]; ok {
		r.mu.Unlock()
		return cached, nil
	}
	// Simple size limit: clear entire cache if it exceeds maxCacheSize
	if len(r.cache) >= maxCacheSize {
		r.cache = make(map[string]*Repo)
	}
	r.cache[cwd] = resolved
	r.mu.Unlock()

	return resolved, nil
}

func (r *Resolver) resolve(cwd, label string) *Repo {
	// A toplevel lookup failure is non-fatal here: the remote URL still
	// uniquely identifies the repo.
	if rawURL, err := r.git.RemoteURL(cwd); err == nil && rawURL != "" {
		id, name, fullURL := normalizeRemoteURL(rawURL)
		toplevel, _ := r.git.Toplevel(cwd)
		return &Repo{ID: id, Name: name, URL: fullURL, Rank: SourceGitRemote, Toplevel: toplevel}
	}

	// Fallback: git toplevel basename
	if toplevel, err := r.git.Toplevel(cwd); err == nil && toplevel != "" {
		base := filepath.Base(toplevel)
		return &Repo{ID: base, Name: base, Rank: SourceGitToplevel, Toplevel: toplevel}
	}

	// Container fallback: label + basename
	base := filepath.Base(cwd)
	if label != "" {
		return &Repo{ID: label + "/" + base, Name: base}
	}

	// Final fallback: basename only
	return &Repo{ID: base, Name: base}
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

// LoadCache populates the in-memory cache from persisted cwd resolutions.
// Loaded entries carry no Name or URL; those come from the repos table.
func (r *Resolver) LoadCache(entries []CacheEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range entries {
		r.cache[e.CWD] = &Repo{ID: e.RepoID, Rank: e.Rank, Toplevel: e.Toplevel}
	}
}

// ClearCache removes all entries from the in-memory cache.
func (r *Resolver) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]*Repo)
}

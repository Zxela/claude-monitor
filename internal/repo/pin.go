package repo

import (
	"path/filepath"
	"strings"
)

// Pin is the Repo a Session is attributed to, plus the provenance needed to
// judge later resolutions against it.
type Pin struct {
	ID       string
	Rank     int
	Toplevel string
	// Inherited means ID came from the Parent session, not this Session's cwd.
	Inherited bool
}

// CacheEntry is one persisted cwd resolution.
type CacheEntry struct {
	CWD      string
	RepoID   string
	Rank     int
	Toplevel string
}

// Inherit points a Child session at its Parent session's repo id. It tracks the
// parent's current id on every call, so a same-repo upgrade on the parent
// reaches the child; start-pinning on the parent keeps that from ever being a
// project switch.
func (p Pin) Inherit(parentRepoID string) Pin {
	p.Inherited = true
	if p.ID != parentRepoID {
		// As trustworthy as the parent's, so the child's own cwd cannot outrank it.
		p.ID, p.Rank, p.Toplevel = parentRepoID, SourceGitRemote, ""
	}
	return p
}

// Resolve applies resolution r of newCWD to a Session that started in startCWD.
// The first resolution pins the Session to its start project. After that, r
// replaces the pin only when it outranks it AND is the same repository; a
// different project never does, whatever its rank. adopted reports whether r is
// now the pin.
func (p Pin) Resolve(r *Repo, startCWD, newCWD string) (pin Pin, adopted bool) {
	if p.ID != "" && (r.ID == p.ID || r.Rank <= p.Rank || !p.sameRepo(r, startCWD, newCWD)) {
		return p, false
	}
	p.ID, p.Rank, p.Toplevel = r.ID, r.Rank, r.Toplevel
	return p, true
}

// CacheEntry returns what to persist for startCWD so a restart resolves it to
// this pin. An inherited pin is not cached: startCWD is the Child session's own
// worktree, and caching it would attribute unrelated Sessions there to the parent.
func (p Pin) CacheEntry(startCWD string) (CacheEntry, bool) {
	if startCWD == "" || p.ID == "" || p.Inherited {
		return CacheEntry{}, false
	}
	return CacheEntry{CWD: startCWD, RepoID: p.ID, Rank: p.Rank, Toplevel: p.Toplevel}, true
}

// sameRepo returns true only on positive evidence, so an ambiguous case keeps
// the pin.
//   - Both git-backed: r's toplevel is the pin's or nested within it. A wider
//     toplevel that contains the pin is an enclosing monorepo, a different
//     project.
//   - Pin has no toplevel (non-git fallback, e.g. git timed out at start): r's
//     toplevel contains the start cwd, or failing that the cwd r came from.
func (p Pin) sameRepo(r *Repo, startCWD, newCWD string) bool {
	if r.Toplevel == "" {
		return false
	}
	if p.Toplevel != "" {
		return pathContains(p.Toplevel, r.Toplevel)
	}
	if startCWD != "" {
		return pathContains(r.Toplevel, startCWD)
	}
	return newCWD != "" && pathContains(r.Toplevel, newCWD)
}

// pathContains compares on a trailing separator so "/repo" is not a prefix of "/repo2".
func pathContains(parent, child string) bool {
	if parent == child {
		return true
	}
	return strings.HasPrefix(child+string(filepath.Separator), parent+string(filepath.Separator))
}

package repo

// Repo represents a resolved repository identity.
type Repo struct {
	ID   string // normalized: "github.com/Zxela/claude-monitor" or fallback
	Name string // human-readable: "claude-monitor"
	URL  string // full remote URL, empty for local-only or fallback
	// FromGit is true when ID/Name/URL were derived from a git remote or git
	// toplevel (authoritative), false for label/basename fallbacks. It is a
	// runtime-only hint used to upgrade an earlier fallback resolution; it is
	// NOT persisted.
	FromGit bool
	// Toplevel is the absolute git working-tree root for this resolution, when
	// known (empty for non-git fallbacks). It is a runtime-only hint used to
	// decide whether a later resolution refers to the SAME repository as the
	// session's pinned one (start-pin upgrade), and is NOT persisted.
	Toplevel string
}

// Resolution-authority ranks, used to decide whether a later resolution should
// upgrade an earlier one for the same session (higher wins).
const (
	SourceFallback    = 0 // container label or bare basename — no git
	SourceGitToplevel = 2 // git toplevel basename (FromGit, no remote URL)
	SourceGitRemote   = 3 // git remote origin (FromGit, has URL) — most authoritative
)

// SourceRank reports how authoritative this resolution is. A git remote (which
// carries a URL) outranks a git toplevel basename, which outranks any non-git
// fallback. Used so a remote-origin resolution can upgrade an earlier
// toplevel-basename one for the same session (not just a non-git fallback).
func (r *Repo) SourceRank() int {
	switch {
	case r.URL != "":
		return SourceGitRemote
	case r.FromGit:
		return SourceGitToplevel
	default:
		return SourceFallback
	}
}

package repo

// Repo represents a resolved repository identity.
type Repo struct {
	ID   string // normalized: "github.com/Zxela/claude-monitor" or fallback
	Name string // human-readable: "claude-monitor"
	URL  string // full remote URL, empty for local-only or fallback
	// Rank is how authoritative this resolution is (Source* constants).
	Rank int
	// Toplevel is the git working-tree root this resolution came from, empty
	// for non-git fallbacks. Pin uses it to tell a same-repo upgrade from a
	// different project.
	Toplevel string
}

// Resolution-authority ranks, used to decide whether a later resolution should
// upgrade an earlier one for the same session (higher wins).
const (
	SourceFallback    = 0 // container label or bare basename — no git
	SourceGitToplevel = 2 // git toplevel basename (no remote URL)
	SourceGitRemote   = 3 // git remote origin (has URL) — most authoritative
)

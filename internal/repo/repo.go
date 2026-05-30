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
}

package repo

// Repo represents a resolved repository identity.
type Repo struct {
	ID   string // normalized: "github.com/Zxela/claude-monitor" or fallback
	Name string // human-readable: "claude-monitor"
	URL  string // full remote URL, empty for local-only or fallback
}

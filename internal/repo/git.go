package repo

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

const gitTimeout = 2 * time.Second

// Git answers the two questions Resolver asks about a working directory.
type Git interface {
	RemoteURL(cwd string) (string, error)
	Toplevel(cwd string) (string, error)
}

// ExecGit runs the git binary.
type ExecGit struct{}

func (ExecGit) RemoteURL(cwd string) (string, error) {
	return runGit(cwd, "remote", "get-url", "origin")
}

func (ExecGit) Toplevel(cwd string) (string, error) {
	return runGit(cwd, "rev-parse", "--show-toplevel")
}

func runGit(cwd string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// FakeGit is an in-memory Git for tests. Each checkout owns every cwd under its
// toplevel; the deepest checkout wins, as with nested git repos. Add checkouts
// before resolving: it is not safe for concurrent writes.
type FakeGit struct {
	remotes map[string]string // toplevel → origin URL, "" for no remote
}

func NewFakeGit() *FakeGit { return &FakeGit{remotes: make(map[string]string)} }

// AddCheckout registers a git working tree; remoteURL "" means no origin.
func (f *FakeGit) AddCheckout(toplevel, remoteURL string) *FakeGit {
	f.remotes[toplevel] = remoteURL
	return f
}

func (f *FakeGit) Toplevel(cwd string) (string, error) {
	best := ""
	for top := range f.remotes {
		if pathContains(top, cwd) && len(top) > len(best) {
			best = top
		}
	}
	if best == "" {
		return "", errors.New("fake git: not a git repository")
	}
	return best, nil
}

func (f *FakeGit) RemoteURL(cwd string) (string, error) {
	top, err := f.Toplevel(cwd)
	if err != nil {
		return "", err
	}
	if f.remotes[top] == "" {
		return "", errors.New("fake git: no such remote 'origin'")
	}
	return f.remotes[top], nil
}

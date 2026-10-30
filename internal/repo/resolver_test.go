package repo

import (
	"testing"
)

func TestResolve_RemoteURLForms(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantID  string
		wantNm  string
		wantURL string
	}{
		{"SCP-style SSH", "git@github.com:Zxela/claude-monitor.git", "github.com/Zxela/claude-monitor", "claude-monitor", "git@github.com:Zxela/claude-monitor.git"},
		{"HTTPS with .git", "https://github.com/Zxela/claude-monitor.git", "github.com/Zxela/claude-monitor", "claude-monitor", "https://github.com/Zxela/claude-monitor.git"},
		{"HTTPS without .git", "https://github.com/Zxela/claude-monitor", "github.com/Zxela/claude-monitor", "claude-monitor", "https://github.com/Zxela/claude-monitor"},
		{"SSH protocol URL", "ssh://git@github.com/Zxela/claude-monitor.git", "github.com/Zxela/claude-monitor", "claude-monitor", "ssh://git@github.com/Zxela/claude-monitor.git"},
		{"GitLab nested groups", "git@gitlab.com:org/team/project.git", "gitlab.com/org/team/project", "project", "git@gitlab.com:org/team/project.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewResolver(NewFakeGit().AddCheckout("/w/project", tt.rawURL))
			got, err := r.Resolve("/w/project/sub", "")
			if err != nil {
				t.Fatal(err)
			}
			want := Repo{ID: tt.wantID, Name: tt.wantNm, URL: tt.wantURL, Rank: SourceGitRemote, Toplevel: "/w/project"}
			if *got != want {
				t.Errorf("got %+v, want %+v", *got, want)
			}
		})
	}
}

func TestResolve_Fallbacks(t *testing.T) {
	git := NewFakeGit().
		AddCheckout("/w/local-only", "").
		AddCheckout("/w/mono", "git@github.com:acme/mono.git").
		AddCheckout("/w/mono/inner", "git@github.com:acme/inner.git")
	tests := []struct {
		name, cwd, label string
		want             Repo
	}{
		{"toplevel without remote", "/w/local-only/pkg", "", Repo{ID: "local-only", Name: "local-only", Rank: SourceGitToplevel, Toplevel: "/w/local-only"}},
		{"nested checkout wins", "/w/mono/inner/x", "", Repo{ID: "github.com/acme/inner", Name: "inner", URL: "git@github.com:acme/inner.git", Rank: SourceGitRemote, Toplevel: "/w/mono/inner"}},
		{"not a repo", "/tmp/my-project", "", Repo{ID: "my-project", Name: "my-project", Rank: SourceFallback}},
		{"not a repo, container label", "/tmp/workspace", "docker-host", Repo{ID: "docker-host/workspace", Name: "workspace", Rank: SourceFallback}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewResolver(git).Resolve(tt.cwd, tt.label)
			if err != nil {
				t.Fatal(err)
			}
			if *got != tt.want {
				t.Errorf("got %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestLoadCache_KeepsRankAndToplevel(t *testing.T) {
	r := NewResolver(NewFakeGit())
	r.LoadCache([]CacheEntry{{CWD: "/w/inner", RepoID: "github.com/acme/inner", Rank: SourceGitRemote, Toplevel: "/w/inner"}})

	got, err := r.Resolve("/w/inner", "")
	if err != nil {
		t.Fatal(err)
	}
	want := Repo{ID: "github.com/acme/inner", Rank: SourceGitRemote, Toplevel: "/w/inner"}
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

func TestClearCache_ResolvesAgain(t *testing.T) {
	git := NewFakeGit()
	r := NewResolver(git)
	r.LoadCache([]CacheEntry{{CWD: "/w/a", RepoID: "stale"}})
	git.AddCheckout("/w/a", "git@github.com:acme/a.git")

	r.ClearCache()

	got, _ := r.Resolve("/w/a", "")
	if got.ID != "github.com/acme/a" {
		t.Errorf("after ClearCache: ID = %q, want github.com/acme/a", got.ID)
	}
}

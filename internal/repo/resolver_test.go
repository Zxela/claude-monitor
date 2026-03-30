package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeRemoteURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantID  string
		wantNm  string
		wantURL string
	}{
		{
			name:    "SCP-style SSH",
			rawURL:  "git@github.com:Zxela/claude-monitor.git",
			wantID:  "github.com/Zxela/claude-monitor",
			wantNm:  "claude-monitor",
			wantURL: "git@github.com:Zxela/claude-monitor.git",
		},
		{
			name:    "HTTPS with .git",
			rawURL:  "https://github.com/Zxela/claude-monitor.git",
			wantID:  "github.com/Zxela/claude-monitor",
			wantNm:  "claude-monitor",
			wantURL: "https://github.com/Zxela/claude-monitor.git",
		},
		{
			name:    "HTTPS without .git",
			rawURL:  "https://github.com/Zxela/claude-monitor",
			wantID:  "github.com/Zxela/claude-monitor",
			wantNm:  "claude-monitor",
			wantURL: "https://github.com/Zxela/claude-monitor",
		},
		{
			name:    "SSH protocol URL",
			rawURL:  "ssh://git@github.com/Zxela/claude-monitor.git",
			wantID:  "github.com/Zxela/claude-monitor",
			wantNm:  "claude-monitor",
			wantURL: "ssh://git@github.com/Zxela/claude-monitor.git",
		},
		{
			name:    "GitLab nested groups",
			rawURL:  "git@gitlab.com:org/team/project.git",
			wantID:  "gitlab.com/org/team/project",
			wantNm:  "project",
			wantURL: "git@gitlab.com:org/team/project.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, name, fullURL := normalizeRemoteURL(tt.rawURL)
			if id != tt.wantID {
				t.Errorf("id = %q, want %q", id, tt.wantID)
			}
			if name != tt.wantNm {
				t.Errorf("name = %q, want %q", name, tt.wantNm)
			}
			if fullURL != tt.wantURL {
				t.Errorf("url = %q, want %q", fullURL, tt.wantURL)
			}
		})
	}
}

func TestResolve_Cache(t *testing.T) {
	r := NewResolver()

	// Pre-populate cache
	r.LoadCache(map[string]string{
		"/home/user/project": "github.com/user/project",
	})

	repo, err := r.Resolve("/home/user/project", "")
	if err != nil {
		t.Fatal(err)
	}
	if repo.ID != "github.com/user/project" {
		t.Errorf("got ID %q, want %q", repo.ID, "github.com/user/project")
	}
}

func TestResolve_GitRepo(t *testing.T) {
	// Use the actual repo we're in as a test
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver()
	repo, err := r.Resolve(cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	// Should resolve to a real repo — ID should contain a host
	if repo.ID == "" {
		t.Error("expected non-empty repo ID")
	}
	if repo.Name == "" {
		t.Error("expected non-empty repo Name")
	}
}

func TestResolve_NonGitFallback(t *testing.T) {
	// Use a temp dir that is not a git repo
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "my-project")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	r := NewResolver()
	repo, err := r.Resolve(sub, "")
	if err != nil {
		t.Fatal(err)
	}
	if repo.ID != "my-project" {
		t.Errorf("got ID %q, want %q", repo.ID, "my-project")
	}
	if repo.Name != "my-project" {
		t.Errorf("got Name %q, want %q", repo.Name, "my-project")
	}
	if repo.URL != "" {
		t.Errorf("got URL %q, want empty", repo.URL)
	}
}

func TestResolve_ContainerFallback(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	r := NewResolver()
	repo, err := r.Resolve(sub, "docker-host")
	if err != nil {
		t.Fatal(err)
	}
	if repo.ID != "docker-host/workspace" {
		t.Errorf("got ID %q, want %q", repo.ID, "docker-host/workspace")
	}
	if repo.Name != "workspace" {
		t.Errorf("got Name %q, want %q", repo.Name, "workspace")
	}
}

func TestClearCache(t *testing.T) {
	r := NewResolver()
	r.LoadCache(map[string]string{
		"/a": "repo-a",
		"/b": "repo-b",
	})

	if len(r.Cache()) != 2 {
		t.Fatalf("expected 2 cache entries, got %d", len(r.Cache()))
	}

	r.ClearCache()

	if len(r.Cache()) != 0 {
		t.Fatalf("expected 0 cache entries after clear, got %d", len(r.Cache()))
	}
}

func TestCache_ReturnsCopy(t *testing.T) {
	r := NewResolver()
	r.LoadCache(map[string]string{"/x": "repo-x"})

	c := r.Cache()
	c["/y"] = &Repo{ID: "repo-y"}

	if len(r.Cache()) != 1 {
		t.Error("modifying returned cache should not affect resolver")
	}
}

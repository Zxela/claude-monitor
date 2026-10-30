package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExecGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available:", err)
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "checkout")
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	var g ExecGit
	if top, err := g.Toplevel(sub); err != nil || top != dir {
		t.Errorf("Toplevel = %q, %v; want %q", top, err, dir)
	}
	if _, err := g.RemoteURL(sub); err == nil {
		t.Error("RemoteURL with no origin: want error")
	}

	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", "git@github.com:acme/checkout.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	if u, err := g.RemoteURL(sub); err != nil || u != "git@github.com:acme/checkout.git" {
		t.Errorf("RemoteURL = %q, %v", u, err)
	}

	if _, err := g.Toplevel(tmp); err == nil {
		t.Error("Toplevel outside a checkout: want error")
	}
}

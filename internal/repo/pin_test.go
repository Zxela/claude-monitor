package repo

import "testing"

func TestPinResolve(t *testing.T) {
	fallback := func(id string) *Repo { return &Repo{ID: id, Rank: SourceFallback} }
	toplevel := func(id, top string) *Repo { return &Repo{ID: id, Rank: SourceGitToplevel, Toplevel: top} }
	remote := func(id, top string) *Repo { return &Repo{ID: id, Rank: SourceGitRemote, Toplevel: top} }
	pinOf := func(r *Repo) Pin { return Pin{ID: r.ID, Rank: r.Rank, Toplevel: r.Toplevel} }

	tests := []struct {
		name     string
		pin      Pin
		incoming *Repo
		startCWD string
		newCWD   string
		want     bool
	}{
		{"first resolution pins a fallback", Pin{}, fallback("widget"), "", "/w/widget", true},
		{"first resolution pins a toplevel", Pin{}, toplevel("alpha", "/w/alpha"), "", "/w/alpha", true},

		{"identical id is a no-op", pinOf(remote("github.com/acme/widget", "/w/widget")), remote("github.com/acme/widget", "/w/widget"), "/w/widget", "/w/widget", false},
		{"lower rank never downgrades", pinOf(remote("github.com/acme/widget", "/w/widget")), toplevel("widget", "/w/widget"), "/w/widget", "/w/widget", false},
		{"fallback never downgrades git", pinOf(remote("github.com/acme/widget", "/w/widget")), fallback("widget"), "/w/widget", "/w/widget", false},

		{"remote upgrades toplevel of the same checkout", pinOf(toplevel("widget", "/w/widget")), remote("github.com/acme/widget", "/w/widget"), "/w/widget", "/w/widget", true},
		{"incoming nested within pin upgrades", pinOf(toplevel("mono", "/w/mono")), remote("github.com/acme/pkg", "/w/mono/pkg"), "/w/mono", "/w/mono/pkg", true},
		{"higher-rank different project never flips", pinOf(toplevel("alpha", "/w/alpha")), remote("github.com/acme/beta", "/w/beta"), "/w/alpha", "/w/beta", false},
		{"enclosing monorepo never flips an inner pin", pinOf(remote("github.com/acme/inner", "/w/mono/inner")), remote("github.com/acme/mono", "/w/mono"), "/w/mono/inner", "/w/mono", false},
		{"enclosing monorepo never flips an inner toplevel pin", pinOf(toplevel("inner", "/w/mono/inner")), remote("github.com/acme/mono", "/w/mono"), "/w/mono/inner", "/w/mono", false},
		{"/repo is not a prefix of /repo2", pinOf(toplevel("repo", "/w/repo")), remote("github.com/acme/repo2", "/w/repo2"), "/w/repo", "/w/repo2", false},
		{"incoming without toplevel has no evidence", pinOf(fallback("widget")), &Repo{ID: "x", Rank: SourceGitRemote}, "/w/widget", "/w/widget", false},

		{"fallback pin upgrades when start cwd is inside", pinOf(fallback("my-project")), remote("github.com/acme/my-project", "/w/my-project"), "/w/my-project/src", "/w/my-project", true},
		{"fallback pin ignores a checkout outside start cwd", pinOf(fallback("foo")), remote("github.com/acme/widget", "/w/widget"), "/elsewhere/foo", "/w/widget", false},
		{"fallback pin, no start cwd, new cwd inside", pinOf(fallback("widget")), remote("github.com/acme/widget", "/w/widget"), "", "/w/widget/cmd", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, adopted := tt.pin.Resolve(tt.incoming, tt.startCWD, tt.newCWD)
			if adopted != tt.want {
				t.Fatalf("adopted = %v, want %v", adopted, tt.want)
			}
			want := tt.pin
			if tt.want {
				want = pinOf(tt.incoming)
			}
			if got != want {
				t.Errorf("pin = %+v, want %+v", got, want)
			}
		})
	}
}

func TestPinResolve_KeepsInherited(t *testing.T) {
	got, _ := Pin{Inherited: true}.Resolve(&Repo{ID: "x"}, "", "/w/x")
	if !got.Inherited {
		t.Error("Resolve cleared Inherited")
	}
}

func TestPinInherit(t *testing.T) {
	own := Pin{ID: "agent-deadbeef", Rank: SourceGitToplevel, Toplevel: "/w/.worktrees/agent-deadbeef"}

	got := own.Inherit("widget")
	want := Pin{ID: "widget", Rank: SourceGitRemote, Inherited: true}
	if got != want {
		t.Fatalf("Inherit = %+v, want %+v", got, want)
	}
	if again := got.Inherit("widget"); again != got {
		t.Errorf("repeat Inherit changed the pin: %+v", again)
	}
	if upgraded := got.Inherit("github.com/acme/widget"); upgraded.ID != "github.com/acme/widget" {
		t.Errorf("child did not track the parent's upgrade: %+v", upgraded)
	}

	// An inherited pin outranks anything the child's own cwd resolves to.
	if _, adopted := got.Resolve(&Repo{ID: "agent-deadbeef", Rank: SourceGitRemote, Toplevel: "/w"}, "/w", "/w"); adopted {
		t.Error("child's own resolution replaced the inherited pin")
	}
}

func TestPinCacheEntry(t *testing.T) {
	pin := Pin{ID: "github.com/acme/widget", Rank: SourceGitRemote, Toplevel: "/w/widget"}
	got, ok := pin.CacheEntry("/w/widget/src")
	want := CacheEntry{CWD: "/w/widget/src", RepoID: "github.com/acme/widget", Rank: SourceGitRemote, Toplevel: "/w/widget"}
	if !ok || got != want {
		t.Errorf("CacheEntry = %+v, %v; want %+v", got, ok, want)
	}

	for name, tc := range map[string]struct {
		pin Pin
		cwd string
	}{
		"inherited child worktree": {Pin{ID: "widget", Inherited: true}, "/w/.worktrees/agent-x"},
		"no cwd":                   {pin, ""},
		"no repo":                  {Pin{}, "/w/widget"},
	} {
		if _, ok := tc.pin.CacheEntry(tc.cwd); ok {
			t.Errorf("%s: CacheEntry ok = true, want false", name)
		}
	}
}

// A cwd resolved before a restart keeps its rank and toplevel, so an enclosing
// repo cannot re-pin a Session that starts there after the restart.
func TestCacheEntry_RoundTripKeepsStartPin(t *testing.T) {
	git := NewFakeGit().
		AddCheckout("/w/mono", "git@github.com:acme/mono.git").
		AddCheckout("/w/mono/inner", "git@github.com:acme/inner.git")

	before, _ := NewResolver(git).Resolve("/w/mono/inner", "")
	first, _ := Pin{}.Resolve(before, "", "/w/mono/inner")
	entry, ok := first.CacheEntry("/w/mono/inner")
	if !ok {
		t.Fatal("no cache entry")
	}

	restarted := NewResolver(git)
	restarted.LoadCache([]CacheEntry{entry})
	cached, _ := restarted.Resolve("/w/mono/inner", "")
	pin, _ := Pin{}.Resolve(cached, "", "/w/mono/inner")
	outer, _ := restarted.Resolve("/w/mono", "")
	pin, _ = pin.Resolve(outer, "/w/mono/inner", "/w/mono")

	if pin.ID != "github.com/acme/inner" {
		t.Errorf("after restart: pinned to %q, want github.com/acme/inner", pin.ID)
	}
}

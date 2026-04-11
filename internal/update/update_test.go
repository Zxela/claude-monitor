package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		current, latest string
		want            int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0", "v2.0.0", -1},
		{"v1.9.0", "v1.10.0", -1},
		{"v1.16.5", "v1.17.0", -1},
	}
	for _, tt := range tests {
		got := compareVersions(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestCheckLatest_NewerAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ghRelease{
			TagName: "v2.0.0",
			HTMLURL: "https://github.com/Zxela/claude-monitor/releases/tag/v2.0.0",
		})
	}))
	defer srv.Close()

	origURL := apiURL
	t.Cleanup(func() { apiURL = origURL })
	apiURL = srv.URL
	rel, err := CheckLatest("v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil {
		t.Fatal("expected release, got nil")
	}
	if rel.Version != "v2.0.0" {
		t.Errorf("version = %q, want %q", rel.Version, "v2.0.0")
	}
	if rel.URL != "https://github.com/Zxela/claude-monitor/releases/tag/v2.0.0" {
		t.Errorf("url = %q", rel.URL)
	}
}

func TestCheckLatest_AlreadyCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ghRelease{
			TagName: "v1.0.0",
			HTMLURL: "https://github.com/Zxela/claude-monitor/releases/tag/v1.0.0",
		})
	}))
	defer srv.Close()

	origURL := apiURL
	t.Cleanup(func() { apiURL = origURL })
	apiURL = srv.URL
	rel, err := CheckLatest("v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if rel != nil {
		t.Errorf("expected nil, got %+v", rel)
	}
}

func TestCheckLatest_DevBuild(t *testing.T) {
	rel, err := CheckLatest("dev")
	if err != nil {
		t.Fatal(err)
	}
	if rel != nil {
		t.Errorf("expected nil for dev build, got %+v", rel)
	}
}

func TestCheckLatest_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	origURL := apiURL
	t.Cleanup(func() { apiURL = origURL })
	apiURL = srv.URL
	_, err := CheckLatest("v1.0.0")
	if err == nil {
		t.Error("expected error on 500")
	}
}

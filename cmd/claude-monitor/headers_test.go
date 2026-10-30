package main

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api", handleSwaggerUI())
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {})
	h := securityHeaders(mux)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Header().Get("Content-Security-Policy"); got != appCSP {
		t.Errorf("app CSP = %q", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api", nil))
	body := rec.Body.String()
	csp := rec.Header().Get("Content-Security-Policy")

	m := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("inline init script not found")
	}
	sum := sha256.Sum256([]byte(m[1]))
	if want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"; !strings.Contains(csp, want) {
		t.Errorf("swagger CSP %q lacks the inline script hash %s", csp, want)
	}
	for _, tag := range regexp.MustCompile(`<(?:script src|link rel="stylesheet" href)="[^"]+"[^>]*>`).FindAllString(body, -1) {
		if !regexp.MustCompile(`swagger-ui-dist@\d+\.\d+\.\d+/`).MatchString(tag) ||
			!strings.Contains(tag, `integrity="sha384-`) || !strings.Contains(tag, `crossorigin="anonymous"`) {
			t.Errorf("asset not pinned with SRI: %s", tag)
		}
	}
	if n := strings.Count(body, "integrity="); n != 2 {
		t.Errorf("found %d SRI-tagged assets, want 2", n)
	}
}

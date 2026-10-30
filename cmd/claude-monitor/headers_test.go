package main

import (
	"net/http"
	"net/http/httptest"
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
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "https://unpkg.com") || !strings.Contains(csp, "'sha256-") {
		t.Errorf("swagger CSP = %q", csp)
	}
	if strings.Contains(rec.Body.String(), "swagger-ui-dist@5/") {
		t.Error("swagger assets must be pinned to an exact version")
	}
}

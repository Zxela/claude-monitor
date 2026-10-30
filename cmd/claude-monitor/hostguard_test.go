package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostGuard(t *testing.T) {
	h := hostGuard([]string{"monitor.lan", "box.local."}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for host, want := range map[string]int{
		"localhost:7700":       200,
		"LOCALHOST:7700":       200,
		"127.0.0.1:7700":       200,
		"[::1]:7700":           200,
		"192.168.1.20:7700":    200,
		"monitor.lan:7700":     200,
		"monitor.lan.:7700":    200,
		"rebind.evil.com:7700": 403,
		"localhost.evil.com":   403,
		"":                     403,
	} {
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %q: got %d, want %d", host, rec.Code, want)
		}
	}
}

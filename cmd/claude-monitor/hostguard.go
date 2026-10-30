package main

import (
	"net"
	"net/http"
	"strings"
)

// hostGuard rejects requests whose Host is a DNS name other than localhost or
// one passed via --allowed-host. A page on attacker.example whose DNS later
// re-resolves to 127.0.0.1 (DNS rebinding) still sends Host: attacker.example,
// and is refused. IP-literal hosts cannot be rebound and are always allowed.
func hostGuard(allowed []string, next http.Handler) http.Handler {
	set := map[string]bool{"localhost": true}
	for _, h := range allowed {
		set[strings.ToLower(h)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host, set) {
			http.Error(w, "host not allowed; start claude-monitor with --allowed-host "+hostname(r.Host), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hostAllowed(host string, set map[string]bool) bool {
	h := hostname(host)
	if h == "" {
		return false
	}
	if net.ParseIP(h) != nil {
		return true
	}
	return set[strings.ToLower(strings.TrimSuffix(h, "."))]
}

func hostname(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(hostport, "[]")
}

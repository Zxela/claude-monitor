package main

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
)

// The UI sets inline style attributes throughout, hence 'unsafe-inline' for
// styles; scripts are same-origin only.
const appCSP = "default-src 'self'; script-src 'self'; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

var swaggerCSP = func() string {
	sum := sha256.Sum256([]byte(swaggerInit))
	return "default-src 'self'; " +
		"script-src https://unpkg.com 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'; " +
		"style-src https://unpkg.com 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; " +
		"frame-ancestors 'none'; base-uri 'none'"
}()

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", appCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

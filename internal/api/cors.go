package api

import (
	"net/http"
	"strings"
)

// CORS wraps next with cross-origin response headers so browser front-ends
// (dashboards) can call the API. origins is a comma-separated allowlist of
// exact origins, or "*" for any; empty disables CORS entirely and returns next
// untouched, which is the default. Wildcard and echoed origins both work with
// the X-API-Key header because it is a plain header, not a cookie: CORS
// credentials semantics never apply.
func CORS(origins string, next http.Handler) http.Handler {
	if origins == "" {
		return next
	}
	allowed := map[string]bool{}
	for _, o := range strings.Split(origins, ",") {
		allowed[strings.TrimSpace(o)] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// The response depends on the Origin header, so caches must key on it.
		w.Header().Add("Vary", "Origin")
		if origin != "" && (allowed["*"] || allowed[origin]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			// Preflight: answer it here; the mux has no OPTIONS routes.
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Idempotency-Key")
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

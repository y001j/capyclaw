package middleware

import (
	"net/http"
	"slices"
)

// CORS returns middleware that validates Origin headers against an explicit allowlist.
// This prevents ClawJacked-style cross-origin WebSocket hijacking.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" && len(allowedOrigins) > 0 {
				allowed := slices.Contains(allowedOrigins, "*") || slices.Contains(allowedOrigins, origin)
				if !allowed {
					http.Error(w, `{"error":"origin not allowed"}`, http.StatusForbidden)
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Tenant-ID, X-Request-ID")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

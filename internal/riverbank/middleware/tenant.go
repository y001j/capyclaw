package middleware

import (
	"context"
	"net/http"
)

// Tenant returns middleware that injects tenant context from the X-Tenant-ID header.
func Tenant() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := r.Header.Get("X-Tenant-ID")

			// If tenant_id was already set by auth middleware, use that
			if existing, ok := r.Context().Value(TenantIDKey).(string); ok && existing != "" {
				next.ServeHTTP(w, r)
				return
			}

			if tenantID != "" {
				ctx := context.WithValue(r.Context(), TenantIDKey, tenantID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

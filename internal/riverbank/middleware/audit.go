package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"CapyClaw/internal/wetland/footprint"
)

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher so SSE streaming works through the audit wrapper.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for interface assertion traversal.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Audit returns middleware that automatically logs audit events for write operations.
func Audit(logger *footprint.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)

			// Only audit write operations and admin reads
			if r.Method == http.MethodGet && !isAuditableGet(r.URL.Path) {
				return
			}
			if r.Method == http.MethodOptions || r.Method == http.MethodHead {
				return
			}

			// Don't audit health checks
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				return
			}

			// Async audit log — don't block the response
			go func() {
				tenantIDStr, _ := r.Context().Value(TenantIDKey).(string)
				tenantID, _ := uuid.Parse(tenantIDStr)

				userIDStr, _ := r.Context().Value(UserIDKey).(string)
				var actorID *uuid.UUID
				if uid, err := uuid.Parse(userIDStr); err == nil {
					actorID = &uid
				}

				reqID, _ := r.Context().Value(RequestIDCtxKey).(string)

				event := &footprint.AuditEvent{
					TenantID:  tenantID,
					ActorID:   actorID,
					ActorType: "user",
					Action:    deriveAction(r.Method, r.URL.Path),
					IPAddress: r.RemoteAddr,
					UserAgent: r.UserAgent(),
					RequestID: reqID,
				}

				_ = logger.Log(context.Background(), event)
			}()
		})
	}
}

// isAuditableGet returns true for GET endpoints that should be audited.
func isAuditableGet(path string) bool {
	return strings.HasPrefix(path, "/api/v1/admin/")
}

// deriveAction maps HTTP method + path to an audit action string.
func deriveAction(method, path string) string {
	// Normalize path by removing IDs
	segments := strings.Split(strings.Trim(path, "/"), "/")
	var resource string
	for _, s := range segments {
		if _, err := uuid.Parse(s); err != nil && s != "api" && s != "v1" {
			resource = s
		}
	}

	switch method {
	case http.MethodPost:
		return resource + ".create"
	case http.MethodPatch, http.MethodPut:
		return resource + ".update"
	case http.MethodDelete:
		return resource + ".delete"
	case http.MethodGet:
		return resource + ".read"
	default:
		return resource + "." + strings.ToLower(method)
	}
}

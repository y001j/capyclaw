package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

const RequestIDHeader = "X-Request-ID"

type requestIDKeyType string

const RequestIDCtxKey requestIDKeyType = "request_id"

// RequestID ensures every request has a unique request ID for tracing and correlation.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(RequestIDHeader)
		if reqID == "" {
			reqID = uuid.New().String()
		}

		w.Header().Set(RequestIDHeader, reqID)
		ctx := context.WithValue(r.Context(), RequestIDCtxKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

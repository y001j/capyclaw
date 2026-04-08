package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"CapyClaw/internal/shared/config"
)

func TestRateLimit_Disabled(t *testing.T) {
	cfg := config.RapidsConfig{Enabled: false}

	handler := RateLimit(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when disabled, got %d", w.Code)
	}
}

func TestRateLimit_NilRedis(t *testing.T) {
	cfg := config.RapidsConfig{Enabled: true}

	handler := RateLimit(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when Redis nil, got %d", w.Code)
	}
}

func TestRateLimit_NoContext(t *testing.T) {
	cfg := config.RapidsConfig{
		Enabled: true,
		DefaultLimits: config.RateLimitDefaults{
			RequestsPerMinute: 60,
			RequestsPerHour:   1000,
		},
	}

	// Even with a real Redis, if no tenant/user in context, should pass through
	handler := RateLimit(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	ctx := context.WithValue(req.Context(), TenantIDKey, "")
	ctx = context.WithValue(ctx, UserIDKey, "")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when no tenant/user, got %d", w.Code)
	}
}

package errors

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestHTTPStatus(t *testing.T) {
	tests := []struct {
		err    *AppError
		expect int
	}{
		{ErrAppNotFound, http.StatusNotFound},
		{ErrAppUnauthorized, http.StatusUnauthorized},
		{ErrAppForbidden, http.StatusForbidden},
		{ErrAppRateLimited, http.StatusTooManyRequests},
		{ErrAppConflict, http.StatusConflict},
		{ErrAppBadRequest, http.StatusBadRequest},
		{ErrAppInternal, http.StatusInternalServerError},
		{&AppError{Code: "unknown"}, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		if got := HTTPStatus(tt.err); got != tt.expect {
			t.Errorf("HTTPStatus(%s) = %d, want %d", tt.err.Code, got, tt.expect)
		}
	}
}

func TestFromPgxError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		if got := FromPgxError(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("no rows", func(t *testing.T) {
		got := FromPgxError(pgx.ErrNoRows)
		if got.Code != "not_found" {
			t.Errorf("expected not_found, got %s", got.Code)
		}
	})

	t.Run("generic error", func(t *testing.T) {
		got := FromPgxError(errors.New("connection refused"))
		if got.Code != "internal" {
			t.Errorf("expected internal, got %s", got.Code)
		}
	})
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, ErrAppNotFound)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
	body := w.Body.String()
	if body == "" {
		t.Error("expected non-empty body")
	}
}

func TestAppErrorWrap(t *testing.T) {
	inner := errors.New("db down")
	wrapped := ErrAppInternal.Wrap(inner)

	if wrapped.Err != inner {
		t.Error("Wrap should set inner error")
	}
	// Original should not be modified
	if ErrAppInternal.Err != nil {
		t.Error("Wrap should not modify original")
	}
}

func TestAppErrorWithMessage(t *testing.T) {
	custom := ErrAppBadRequest.WithMessage("custom msg")
	if custom.Message != "custom msg" {
		t.Errorf("expected 'custom msg', got %s", custom.Message)
	}
	// Original should not be modified
	if ErrAppBadRequest.Message != "invalid request" {
		t.Error("WithMessage should not modify original")
	}
}

func TestAppErrorWithDetails(t *testing.T) {
	details := map[string]any{"field": "name"}
	custom := ErrAppBadRequest.WithDetails(details)
	if custom.Details["field"] != "name" {
		t.Error("WithDetails should set details")
	}
	if ErrAppBadRequest.Details != nil {
		t.Error("WithDetails should not modify original")
	}
}

package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Standard sentinel errors.
var (
	ErrNotFound            = errors.New("not found")
	ErrAlreadyExists       = errors.New("already exists")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrForbidden           = errors.New("forbidden")
	ErrRateLimited         = errors.New("rate limited")
	ErrBudgetExceeded      = errors.New("budget exceeded")
	ErrTenantSuspended     = errors.New("tenant suspended")
	ErrProviderUnavailable = errors.New("provider unavailable")
	ErrSandboxError        = errors.New("sandbox error")
	ErrValidation          = errors.New("validation error")
)

// AppError is a structured error with code and context.
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
	Details map[string]any `json:"details,omitempty"`
}

// Predefined AppErrors for common cases.
var (
	ErrAppNotFound     = &AppError{Code: "not_found", Message: "resource not found"}
	ErrAppUnauthorized = &AppError{Code: "unauthorized", Message: "authentication required"}
	ErrAppForbidden    = &AppError{Code: "forbidden", Message: "insufficient permissions"}
	ErrAppRateLimited  = &AppError{Code: "rate_limited", Message: "rate limit exceeded"}
	ErrAppConflict     = &AppError{Code: "conflict", Message: "resource already exists"}
	ErrAppBadRequest   = &AppError{Code: "bad_request", Message: "invalid request"}
	ErrAppInternal     = &AppError{Code: "internal", Message: "internal server error"}
)

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

// New creates a new AppError.
func New(code string, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

// WithDetails attaches extra context to the error.
func (e *AppError) WithDetails(details map[string]any) *AppError {
	clone := *e
	clone.Details = details
	return &clone
}

// WithMessage returns a copy with a custom message.
func (e *AppError) WithMessage(msg string) *AppError {
	clone := *e
	clone.Message = msg
	return &clone
}

// Wrap returns a copy wrapping the given error.
func (e *AppError) Wrap(err error) *AppError {
	clone := *e
	clone.Err = err
	return &clone
}

// HTTPStatus returns the HTTP status code for an AppError.
func HTTPStatus(err *AppError) int {
	switch err.Code {
	case "not_found":
		return http.StatusNotFound
	case "unauthorized":
		return http.StatusUnauthorized
	case "forbidden":
		return http.StatusForbidden
	case "rate_limited":
		return http.StatusTooManyRequests
	case "conflict":
		return http.StatusConflict
	case "bad_request":
		return http.StatusBadRequest
	case "internal":
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// FromPgxError converts pgx/pgconn errors to AppError.
func FromPgxError(err error) *AppError {
	if err == nil {
		return nil
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAppNotFound.Wrap(err)
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrAppConflict.Wrap(err)
		case "23503": // foreign_key_violation
			return ErrAppBadRequest.WithMessage("referenced resource not found").Wrap(err)
		case "23502": // not_null_violation
			return ErrAppBadRequest.WithMessage("missing required field: " + pgErr.ColumnName).Wrap(err)
		}
	}

	return ErrAppInternal.Wrap(err)
}

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, appErr *AppError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(HTTPStatus(appErr))

	resp := map[string]any{
		"error": map[string]any{
			"code":    appErr.Code,
			"message": appErr.Message,
		},
	}

	json.NewEncoder(w).Encode(resp)
}

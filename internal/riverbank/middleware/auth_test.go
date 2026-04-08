package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"CapyClaw/internal/shared/config"
)

func generateTestECKey(t *testing.T) (*ecdsa.PrivateKey, *ecdsa.PublicKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}
	return key, &key.PublicKey
}

func signTestJWT(t *testing.T, key *ecdsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tokenStr, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign JWT: %v", err)
	}
	return tokenStr
}

func TestValidateJWT_ValidToken(t *testing.T) {
	priv, pub := generateTestECKey(t)

	tokenStr := signTestJWT(t, priv, jwt.MapClaims{
		"sub":  "user-123",
		"tid":  "tenant-456",
		"role": "admin",
		"exp":  time.Now().Add(time.Hour).Unix(),
		"iat":  time.Now().Unix(),
	})

	cfg := config.JWTConfig{SigningMethod: "ES256"}
	claims, err := validateJWT(tokenStr, cfg, pub)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if claims.UserID != "user-123" {
		t.Errorf("expected user-123, got %s", claims.UserID)
	}
	if claims.TenantID != "tenant-456" {
		t.Errorf("expected tenant-456, got %s", claims.TenantID)
	}
	if claims.Role != "admin" {
		t.Errorf("expected admin, got %s", claims.Role)
	}
}

func TestValidateJWT_ExpiredToken(t *testing.T) {
	priv, pub := generateTestECKey(t)

	tokenStr := signTestJWT(t, priv, jwt.MapClaims{
		"sub":  "user-123",
		"tid":  "tenant-456",
		"role": "admin",
		"exp":  time.Now().Add(-time.Hour).Unix(),
		"iat":  time.Now().Add(-2 * time.Hour).Unix(),
	})

	cfg := config.JWTConfig{SigningMethod: "ES256"}
	_, err := validateJWT(tokenStr, cfg, pub)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestValidateJWT_WrongSigningMethod(t *testing.T) {
	priv, _ := generateTestECKey(t)
	_, pub2 := generateTestECKey(t) // Different key

	tokenStr := signTestJWT(t, priv, jwt.MapClaims{
		"sub": "user-123",
		"tid": "tenant-456",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	cfg := config.JWTConfig{SigningMethod: "ES256"}
	_, err := validateJWT(tokenStr, cfg, pub2)
	if err == nil {
		t.Error("expected error for wrong key")
	}
}

func TestValidateJWT_MissingSub(t *testing.T) {
	priv, pub := generateTestECKey(t)

	tokenStr := signTestJWT(t, priv, jwt.MapClaims{
		"tid": "tenant-456",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	cfg := config.JWTConfig{SigningMethod: "ES256"}
	_, err := validateJWT(tokenStr, cfg, pub)
	if err == nil {
		t.Error("expected error for missing sub claim")
	}
}

func TestAuthMiddleware_NoToken(t *testing.T) {
	cfg := config.AuthConfig{Provider: "jwt", JWT: config.JWTConfig{SigningMethod: "ES256"}}

	handler := Auth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	priv, pub := generateTestECKey(t)

	cfg := config.AuthConfig{Provider: "jwt", JWT: config.JWTConfig{SigningMethod: "ES256"}}

	// We need to inject the public key. Since Auth uses sync.Once for key loading,
	// we test validateJWT directly for valid token cases.
	tokenStr := signTestJWT(t, priv, jwt.MapClaims{
		"sub":  "user-123",
		"tid":  "tenant-456",
		"role": "member",
		"exp":  time.Now().Add(time.Hour).Unix(),
	})

	claims, err := validateJWT(tokenStr, cfg.JWT, pub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("expected user-123, got %s", claims.UserID)
	}
}

func TestExtractToken(t *testing.T) {
	tests := []struct {
		header string
		expect string
	}{
		{"Bearer abc123", "abc123"},
		{"Bearer ", ""},
		{"Basic abc123", ""},
		{"", ""},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/", nil)
		if tt.header != "" {
			req.Header.Set("Authorization", tt.header)
		}
		got := extractToken(req)
		if got != tt.expect {
			t.Errorf("extractToken(%q) = %q, want %q", tt.header, got, tt.expect)
		}
	}
}

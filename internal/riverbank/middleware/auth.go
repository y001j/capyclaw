package middleware

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"

	"CapyClaw/internal/shared/config"
)

type contextKey string

const (
	UserIDKey   contextKey = "user_id"
	TenantIDKey contextKey = "tenant_id"
	RoleKey     contextKey = "role"
)

// Auth returns middleware that validates authentication tokens.
func Auth(cfg config.AuthConfig) func(http.Handler) http.Handler {
	var (
		publicKey    any
		keyOnce      sync.Once
		keyErr       error
		oidcVerifier *oidc.IDTokenVerifier
		oidcOnce     sync.Once
		oidcErr      error
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				http.Error(w, `{"error":{"code":"unauthorized","message":"missing authentication token"}}`, http.StatusUnauthorized)
				return
			}

			switch cfg.Provider {
			case "jwt":
				// Lazy-load the public key once
				keyOnce.Do(func() {
					publicKey, keyErr = resolvePublicKey(cfg.JWT)
				})
				if keyErr != nil {
					slog.Error("JWT key initialization failed", "error", keyErr)
					http.Error(w, `{"error":{"code":"internal","message":"auth configuration error"}}`, http.StatusInternalServerError)
					return
				}

				claims, err := validateJWT(token, cfg.JWT, publicKey)
				if err != nil {
					slog.Warn("JWT validation failed", "error", err)
					http.Error(w, `{"error":{"code":"unauthorized","message":"invalid token"}}`, http.StatusUnauthorized)
					return
				}
				ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
				ctx = context.WithValue(ctx, TenantIDKey, claims.TenantID)
				ctx = context.WithValue(ctx, RoleKey, claims.Role)
				next.ServeHTTP(w, r.WithContext(ctx))

			case "oidc":
				// Lazy-initialize OIDC provider and verifier
				oidcOnce.Do(func() {
					provider, err := oidc.NewProvider(context.Background(), cfg.OIDC.IssuerURL)
					if err != nil {
						oidcErr = fmt.Errorf("initializing OIDC provider from %s: %w", cfg.OIDC.IssuerURL, err)
						return
					}
					oidcVerifier = provider.Verifier(&oidc.Config{
						ClientID: cfg.OIDC.ClientID,
					})
				})
				if oidcErr != nil {
					slog.Error("OIDC initialization failed", "error", oidcErr)
					http.Error(w, `{"error":{"code":"internal","message":"OIDC configuration error"}}`, http.StatusInternalServerError)
					return
				}

				idToken, err := oidcVerifier.Verify(r.Context(), token)
				if err != nil {
					slog.Warn("OIDC token validation failed", "error", err)
					http.Error(w, `{"error":{"code":"unauthorized","message":"invalid OIDC token"}}`, http.StatusUnauthorized)
					return
				}

				// Extract claims from the ID token
				var oidcClaims struct {
					Sub      string `json:"sub"`
					Email    string `json:"email"`
					TenantID string `json:"tid"`
					Role     string `json:"role"`
				}
				if err := idToken.Claims(&oidcClaims); err != nil {
					slog.Warn("OIDC claims extraction failed", "error", err)
					http.Error(w, `{"error":{"code":"unauthorized","message":"invalid token claims"}}`, http.StatusUnauthorized)
					return
				}

				userID := oidcClaims.Sub
				if userID == "" {
					http.Error(w, `{"error":{"code":"unauthorized","message":"missing sub claim"}}`, http.StatusUnauthorized)
					return
				}

				ctx := context.WithValue(r.Context(), UserIDKey, userID)
				ctx = context.WithValue(ctx, TenantIDKey, oidcClaims.TenantID)
				ctx = context.WithValue(ctx, RoleKey, oidcClaims.Role)
				next.ServeHTTP(w, r.WithContext(ctx))

			case "device-pairing":
				// TODO: implement device pairing validation
				next.ServeHTTP(w, r)

			default:
				http.Error(w, `{"error":{"code":"internal","message":"unsupported auth provider"}}`, http.StatusInternalServerError)
			}
		})
	}
}

// JWTClaims holds the validated JWT claims.
type JWTClaims struct {
	UserID   string
	TenantID string
	Role     string
}

// resolvePublicKey loads a public key from file, or generates a dev key pair if path is empty.
func resolvePublicKey(cfg config.JWTConfig) (any, error) {
	if cfg.PublicKeyPath != "" {
		return loadPublicKey(cfg.PublicKeyPath, cfg.SigningMethod)
	}

	// Dev mode: generate ephemeral key pair (shared with EnsureDevKey)
	slog.Warn("no JWT public key configured, generating ephemeral dev key pair — DO NOT USE IN PRODUCTION")
	EnsureDevKey(cfg.SigningMethod)
	if devPublicKey == nil {
		return nil, fmt.Errorf("failed to generate dev key")
	}
	return devPublicKey, nil
}

// loadPublicKey loads a PEM-encoded public key from disk.
func loadPublicKey(path string, method string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading public key file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing public key: %w", err)
	}

	// Verify the key type matches the signing method
	switch method {
	case "ES256":
		if _, ok := pub.(*ecdsa.PublicKey); !ok {
			return nil, fmt.Errorf("expected ECDSA public key for ES256, got %T", pub)
		}
	case "RS256":
		if _, ok := pub.(*rsa.PublicKey); !ok {
			return nil, fmt.Errorf("expected RSA public key for RS256, got %T", pub)
		}
	case "EdDSA":
		if _, ok := pub.(ed25519.PublicKey); !ok {
			return nil, fmt.Errorf("expected Ed25519 public key for EdDSA, got %T", pub)
		}
	}

	return pub, nil
}

// generateDevKey generates an ephemeral key pair for development.
func generateDevKey(method string) (any, error) {
	switch method {
	case "ES256":
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		// Store the private key globally so dev token generation can use it
		devPrivateKey = key
		return &key.PublicKey, nil
	case "RS256":
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, err
		}
		devPrivateKey = key
		return &key.PublicKey, nil
	case "EdDSA":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		devPrivateKey = priv
		return pub, nil
	default:
		return nil, fmt.Errorf("unsupported signing method: %s", method)
	}
}

// devPrivateKey holds the ephemeral private key in dev mode (for generating test tokens).
var (
	devPrivateKey any
	devPublicKey  any
	devKeyOnce    sync.Once
)

// DevPrivateKey returns the dev private key (nil in production).
func DevPrivateKey() any {
	return devPrivateKey
}

// EnsureDevKey generates the ephemeral dev key pair if not already initialized.
func EnsureDevKey(signingMethod string) {
	devKeyOnce.Do(func() {
		pub, _ := generateDevKey(signingMethod)
		devPublicKey = pub
	})
}

// DevPublicKey returns the dev public key (nil in production).
func DevPublicKey() any {
	return devPublicKey
}

// SigningMethodFromString returns the jwt.SigningMethod for the given config string.
func SigningMethodFromString(method string) jwt.SigningMethod {
	switch method {
	case "ES256":
		return jwt.SigningMethodES256
	case "RS256":
		return jwt.SigningMethodRS256
	case "EdDSA":
		return jwt.SigningMethodEdDSA
	default:
		return jwt.SigningMethodES256
	}
}

// validateJWT validates a JWT token and extracts claims.
func validateJWT(tokenStr string, cfg config.JWTConfig, publicKey any) (*JWTClaims, error) {
	expectedMethod := SigningMethodFromString(cfg.SigningMethod)

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		// Strict algorithm check to prevent algorithm confusion attacks
		if token.Method.Alg() != expectedMethod.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v, expected %v", token.Method.Alg(), expectedMethod.Alg())
		}
		return publicKey, nil
	},
		jwt.WithValidMethods([]string{expectedMethod.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("parsing token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}

	claims := &JWTClaims{}

	// Extract sub → UserID
	if sub, ok := mapClaims["sub"].(string); ok {
		claims.UserID = sub
	}

	// Extract tid → TenantID
	if tid, ok := mapClaims["tid"].(string); ok {
		claims.TenantID = tid
	}

	// Extract role → Role
	if role, ok := mapClaims["role"].(string); ok {
		claims.Role = role
	}

	if claims.UserID == "" {
		return nil, fmt.Errorf("missing sub claim")
	}

	return claims, nil
}

// ValidateJWTToken validates a raw JWT token string for WebSocket authentication.
// This is the public entry point used by the WebSocket handler.
func ValidateJWTToken(tokenStr string, cfg config.AuthConfig) (*JWTClaims, error) {
	pubKey, err := resolvePublicKey(cfg.JWT)
	if err != nil {
		return nil, fmt.Errorf("resolving public key: %w", err)
	}
	return validateJWT(tokenStr, cfg.JWT, pubKey)
}

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

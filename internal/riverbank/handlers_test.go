package riverbank

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"CapyClaw/internal/riverbank/middleware"
	"CapyClaw/internal/shared/config"
)

// setupTestServer creates a Server connected to the dev DB and Redis.
// Requires docker-compose.dev.yml to be running.
func setupTestServer(t *testing.T) (*Server, func()) {
	t.Helper()

	cfg := &config.Config{
		Version:  "1",
		LogLevel: "debug",
		Riverbank: config.RiverbankConfig{
			Host: "0.0.0.0",
			Port: 18789,
			Auth: config.AuthConfig{
				Provider: "jwt",
				JWT:      config.JWTConfig{SigningMethod: "ES256"},
			},
		},
		Rapids: config.RapidsConfig{
			Enabled: true,
			DefaultLimits: config.RateLimitDefaults{
				RequestsPerMinute: 60,
				RequestsPerHour:   1000,
			},
		},
		Burrow: config.BurrowConfig{
			DefaultModel: "claude-sonnet-4-20250514",
		},
		Pond: config.PondConfig{
			Postgres: config.PostgresConfig{
				Host:     "localhost",
				Port:     18700,
				Database: "capyclaw",
				Username: "capyclaw",
				Password: "capyclaw_dev",
				SSLMode:  "disable",
				Pool: config.PoolConfig{
					MaxConns: 5,
					MinConns: 1,
				},
			},
		},
		Lodge: config.LodgeConfig{
			Redis: config.RedisConfig{
				Addresses: []string{"localhost:18800"},
			},
		},
	}

	ctx := context.Background()
	srv, err := NewServer(ctx, cfg)
	if err != nil {
		t.Skipf("skipping: cannot connect to dev infra: %v", err)
	}

	return srv, func() {
		srv.Shutdown(context.Background())
	}
}

// signTestToken creates a JWT signed with the dev key.
func signTestToken(t *testing.T, userID, tenantID, role string) string {
	t.Helper()

	privKey := middleware.DevPrivateKey()
	if privKey == nil {
		t.Fatal("dev private key not available")
	}

	claims := jwt.MapClaims{
		"sub":  userID,
		"tid":  tenantID,
		"role": role,
		"exp":  time.Now().Add(time.Hour).Unix(),
		"iat":  time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	signed, err := token.SignedString(privKey)
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return signed
}

func TestHealthEndpoints(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("healthz", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/healthz", nil)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("expected 200, got %d", w.Code)
		}
		var body map[string]string
		json.Unmarshal(w.Body.Bytes(), &body)
		if body["status"] != "ok" {
			t.Errorf("expected ok, got %s", body["status"])
		}
	})

	t.Run("readyz", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/readyz", nil)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var body map[string]any
		json.Unmarshal(w.Body.Bytes(), &body)
		if body["postgres"] != "up" {
			t.Errorf("postgres=%v", body["postgres"])
		}
		if body["redis"] != "up" {
			t.Errorf("redis=%v", body["redis"])
		}
	})
}

func TestAuthFlow(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("no token → 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/agents", nil)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("invalid token → 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/agents", nil)
		req.Header.Set("Authorization", "Bearer invalid.jwt.token")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("valid token → passes auth", func(t *testing.T) {
		// Trigger dev key generation
		triggerReq := httptest.NewRequest("GET", "/api/v1/agents", nil)
		triggerReq.Header.Set("Authorization", "Bearer trigger-key-gen")
		triggerW := httptest.NewRecorder()
		srv.router.ServeHTTP(triggerW, triggerReq)

		token := signTestToken(t, "00000000-0000-0000-0000-000000000002", "00000000-0000-0000-0000-000000000001", "admin")
		req := httptest.NewRequest("GET", "/api/v1/agents", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestAgentCRUD(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Trigger dev key generation by hitting an auth endpoint
	triggerReq := httptest.NewRequest("GET", "/api/v1/agents", nil)
	triggerReq.Header.Set("Authorization", "Bearer trigger")
	triggerW := httptest.NewRecorder()
	srv.router.ServeHTTP(triggerW, triggerReq)

	// The dev key needs to be generated for signing tokens.
	// Since the Auth middleware uses sync.Once at package level, we may
	// already have a key from a previous test. Generate one if missing.
	if middleware.DevPrivateKey() == nil {
		t.Skip("dev key not available for CRUD tests")
	}

	token := signTestToken(t, "00000000-0000-0000-0000-000000000002", "00000000-0000-0000-0000-000000000001", "admin")
	auth := "Bearer " + token

	slug := fmt.Sprintf("test-agent-%d", time.Now().UnixNano())

	// Create
	var agentID string
	t.Run("create agent", func(t *testing.T) {
		body := fmt.Sprintf(`{"name":"Test Agent","slug":"%s","model":"claude-sonnet-4-20250514"}`, slug)
		req := httptest.NewRequest("POST", "/api/v1/agents", strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != 201 {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		agentID = result["id"].(string)
		t.Logf("Created agent: %s", agentID)

		if result["name"] != "Test Agent" {
			t.Errorf("name=%v", result["name"])
		}
		if result["status"] != "active" {
			t.Errorf("status=%v", result["status"])
		}
	})

	// Get
	t.Run("get agent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/agents/"+agentID, nil)
		req.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["id"] != agentID {
			t.Errorf("id=%v", result["id"])
		}
	})

	// List
	t.Run("list agents", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/agents", nil)
		req.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var result []map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		found := false
		for _, a := range result {
			if a["id"] == agentID {
				found = true
			}
		}
		if !found {
			t.Error("created agent not in list")
		}
		t.Logf("Found %d agents", len(result))
	})

	// Update
	t.Run("update agent", func(t *testing.T) {
		body := `{"model":"gpt-4o","system_prompt":"You are helpful"}`
		req := httptest.NewRequest("PATCH", "/api/v1/agents/"+agentID, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["model"] != "gpt-4o" {
			t.Errorf("model=%v, want gpt-4o", result["model"])
		}
	})

	// Delete (soft)
	t.Run("soft delete agent", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/agents/"+agentID, nil)
		req.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	// Verify archived agent not in list
	t.Run("archived agent excluded from list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/agents", nil)
		req.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		var result []map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		for _, a := range result {
			if a["id"] == agentID {
				t.Error("archived agent still in list")
			}
		}
	})
}

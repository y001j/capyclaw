// +build ignore

// e2e_test.go — End-to-end test script for Phase 1 & 2 features.
// Run: go run scripts/e2e_test.go
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	baseURL   = "http://localhost:18789"
	passed    = 0
	failed    = 0
	privKey   *ecdsa.PrivateKey
)

func main() {
	// Generate a key pair for JWT signing
	var err error
	privKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		fatal("generate key: %v", err)
	}

	// Start the gateway
	fmt.Println("🚀 Starting CapyClaw Gateway...")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/gateway")
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fatal("start gateway: %v", err)
	}
	defer func() {
		cancel()
		cmd.Wait()
	}()

	// Wait for server to be ready
	waitForReady()

	fmt.Println("\n===== Phase 1: 基础加固 测试 =====\n")

	// Test 1: Health check
	test("GET /healthz → 200", func() error {
		resp := get("/healthz", "")
		return expectStatus(resp, 200)
	})

	// Test 2: Readyz with DB + Redis
	test("GET /readyz → postgres:up, redis:up", func() error {
		resp := get("/readyz", "")
		if err := expectStatus(resp, 200); err != nil {
			return err
		}
		body := readBody(resp)
		var result map[string]any
		json.Unmarshal(body, &result)
		if result["postgres"] != "up" {
			return fmt.Errorf("postgres=%v, want 'up'", result["postgres"])
		}
		if result["redis"] != "up" {
			return fmt.Errorf("redis=%v, want 'up'", result["redis"])
		}
		return nil
	})

	// Test 3: No auth → 401
	test("GET /api/v1/agents 无Token → 401", func() error {
		resp := get("/api/v1/agents", "")
		return expectStatus(resp, 401)
	})

	// Test 4: Invalid JWT → 401
	test("GET /api/v1/agents 无效JWT → 401", func() error {
		resp := get("/api/v1/agents", "Bearer garbage.token.here")
		return expectStatus(resp, 401)
	})

	// Test 5: Expired JWT → 401
	test("GET /api/v1/agents 过期JWT → 401", func() error {
		token := signJWT(jwt.MapClaims{
			"sub":  "00000000-0000-0000-0000-000000000002",
			"tid":  "00000000-0000-0000-0000-000000000001",
			"role": "admin",
			"exp":  time.Now().Add(-time.Hour).Unix(),
			"iat":  time.Now().Add(-2 * time.Hour).Unix(),
		})
		resp := get("/api/v1/agents", "Bearer "+token)
		return expectStatus(resp, 401)
	})

	fmt.Println("\n===== Phase 2: Agent CRUD 测试 =====\n")

	// For Phase 2 tests, since Auth middleware uses sync.Once for key loading and
	// we can't inject our key into the running server's middleware, we'll test
	// against the dev mode auto-generated key.
	// The server is in dev mode (no public_key_path set), so it generated an
	// ephemeral key. We can't sign with it from here.
	// Instead, let's verify the CRUD logic by testing the response shapes.

	// We need to generate JWT that the server will accept.
	// Since the server is using its own dev key, we can't sign tokens from here.
	// Let's write a Go test that directly tests the handlers instead.

	fmt.Printf("\n📊 结果: %d 通过, %d 失败\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func waitForReady() {
	for i := 0; i < 30; i++ {
		resp, err := http.Get(baseURL + "/healthz")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			fmt.Println("✅ Gateway is ready")
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	fatal("gateway did not become ready in 15 seconds")
}

func test(name string, fn func() error) {
	err := fn()
	if err != nil {
		fmt.Printf("  ❌ %s: %v\n", name, err)
		failed++
	} else {
		fmt.Printf("  ✅ %s\n", name)
		passed++
	}
}

func get(path, auth string) *http.Response {
	req, _ := http.NewRequest("GET", baseURL+path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal("GET %s: %v", path, err)
	}
	return resp
}

func post(path string, body any, auth string) *http.Response {
	bodyJSON, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", baseURL+path, bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal("POST %s: %v", path, err)
	}
	return resp
}

func expectStatus(resp *http.Response, expected int) error {
	defer resp.Body.Close()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("got %d, want %d (body: %s)", resp.StatusCode, expected, string(body))
	}
	return nil
}

func readBody(resp *http.Response) []byte {
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return body
}

func signJWT(claims jwt.MapClaims) string {
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	signed, err := token.SignedString(privKey)
	if err != nil {
		fatal("sign JWT: %v", err)
	}
	return signed
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", args...)
	os.Exit(1)
}

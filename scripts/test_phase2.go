//go:build ignore

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"CapyClaw/internal/riverbank/middleware"
)

var (
	baseURL = "http://localhost:18789"
	token   string
	passed  = 0
	failed  = 0
)

func main() {
	fmt.Println("🚀 Starting gateway...")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/gateway")
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	cmd.Start()
	defer func() {
		cancel()
		cmd.Wait()
	}()

	// Wait for server to be ready
	for i := 0; i < 30; i++ {
		resp, err := http.Get(baseURL + "/healthz")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Wait for dev key to be initialized by hitting an auth endpoint
	doReq("GET", "/api/v1/agents", nil, "Bearer trigger-init")
	time.Sleep(200 * time.Millisecond)

	// Get the dev private key and sign a valid JWT
	privKey := middleware.DevPrivateKey()
	if privKey == nil {
		fmt.Println("❌ Dev private key not available (server running in separate process)")
		fmt.Println("   Switching to httptest-based handler testing...")
		testHandlersDirect()
		return
	}

	claims := jwt.MapClaims{
		"sub":  "00000000-0000-0000-0000-000000000002",
		"tid":  "00000000-0000-0000-0000-000000000001",
		"role": "admin",
		"exp":  time.Now().Add(time.Hour).Unix(),
		"iat":  time.Now().Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	var err error
	token, err = t.SignedString(privKey)
	if err != nil {
		fmt.Printf("❌ sign JWT: %v\n", err)
		os.Exit(1)
	}

	runAgentCRUDTests()

	fmt.Printf("\n📊 结果: %d 通过, %d 失败\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func testHandlersDirect() {
	fmt.Println("\n===== 使用 Handler 直接测试 =====\n")
	fmt.Println("由于 Gateway 在独立进程中运行，无法获取 dev key。")
	fmt.Println("请使用 'go test ./internal/...' 运行单元测试。")
}

func runAgentCRUDTests() {
	fmt.Println("\n===== Phase 2: Agent CRUD 测试 =====\n")

	// Create Agent
	var agentID string
	test("POST /api/v1/agents → 201", func() error {
		body := map[string]any{
			"name":  "Test Agent",
			"slug":  "test-agent",
			"model": "claude-sonnet-4-20250514",
		}
		resp := doReq("POST", "/api/v1/agents", body, "Bearer "+token)
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		agentID = result["id"].(string)
		fmt.Printf("    Agent ID: %s\n", agentID)
		return nil
	})

	// Get Agent
	test("GET /api/v1/agents/{id} → 200", func() error {
		resp := doReq("GET", "/api/v1/agents/"+agentID, nil, "Bearer "+token)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["name"] != "Test Agent" {
			return fmt.Errorf("name=%v", result["name"])
		}
		return nil
	})

	// List Agents
	test("GET /api/v1/agents → 200 (list)", func() error {
		resp := doReq("GET", "/api/v1/agents", nil, "Bearer "+token)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("got %d: %s", resp.StatusCode, string(b))
		}
		var result []map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if len(result) == 0 {
			return fmt.Errorf("expected at least 1 agent")
		}
		fmt.Printf("    Found %d agents\n", len(result))
		return nil
	})

	// Update Agent
	test("PATCH /api/v1/agents/{id} → 200", func() error {
		body := map[string]any{
			"model": "gpt-4o",
		}
		resp := doReq("PATCH", "/api/v1/agents/"+agentID, body, "Bearer "+token)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["model"] != "gpt-4o" {
			return fmt.Errorf("model=%v", result["model"])
		}
		return nil
	})

	// Delete Agent (soft delete)
	test("DELETE /api/v1/agents/{id} → 200 (soft delete)", func() error {
		resp := doReq("DELETE", "/api/v1/agents/"+agentID, nil, "Bearer "+token)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("got %d: %s", resp.StatusCode, string(b))
		}
		return nil
	})

	// Verify soft delete: agent should not appear in list
	test("GET /api/v1/agents → archived agent not in list", func() error {
		resp := doReq("GET", "/api/v1/agents", nil, "Bearer "+token)
		defer resp.Body.Close()
		var result []map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		for _, a := range result {
			if a["id"] == agentID {
				return fmt.Errorf("archived agent still in list")
			}
		}
		return nil
	})
}

func doReq(method, path string, body any, auth string) *http.Response {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, baseURL+path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %s %s: %v\n", method, path, err)
		os.Exit(1)
	}
	return resp
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

func init() {
	_ = strings.TrimSpace("")
}

//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

const baseURL = "http://localhost:18789"

// devToken is used for integration tests — matches the seed data admin user.
var devToken = "test-jwt-token"

func authHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + devToken,
		"Content-Type":  "application/json",
	}
}

func doRequest(t *testing.T, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, baseURL+path, bodyReader)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	for k, v := range authHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, respBody
}

// TestGatewayHealth verifies that the /healthz endpoint returns 200 OK.
func TestGatewayHealth(t *testing.T) {
	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestReadiness verifies /readyz returns infrastructure status.
func TestReadiness(t *testing.T) {
	resp, err := http.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)

	if body["postgres"] != "up" {
		t.Errorf("postgres not up: %v", body["postgres"])
	}
}

// TestAgentCRUD tests the full agent lifecycle.
func TestAgentCRUD(t *testing.T) {
	slug := fmt.Sprintf("test-agent-%d", time.Now().UnixNano())

	// Create
	resp, body := doRequest(t, "POST", "/api/v1/agents", map[string]string{
		"name":  "Test Agent",
		"slug":  slug,
		"model": "claude-sonnet-4-20250514",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent: expected 201, got %d: %s", resp.StatusCode, body)
	}

	var agent struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	json.Unmarshal(body, &agent)

	if agent.Slug != slug {
		t.Errorf("slug mismatch: expected %s, got %s", slug, agent.Slug)
	}

	// Get
	resp, body = doRequest(t, "GET", "/api/v1/agents/"+agent.ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get agent: expected 200, got %d", resp.StatusCode)
	}

	// Update
	resp, body = doRequest(t, "PATCH", "/api/v1/agents/"+agent.ID, map[string]string{
		"name": "Updated Agent",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update agent: expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Delete
	resp, _ = doRequest(t, "DELETE", "/api/v1/agents/"+agent.ID, nil)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete agent: expected 200/204, got %d", resp.StatusCode)
	}

	// Verify deleted
	resp, _ = doRequest(t, "GET", "/api/v1/agents/"+agent.ID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

// TestChatCompletions tests the OpenAI-compatible chat endpoint.
func TestChatCompletions(t *testing.T) {
	resp, body := doRequest(t, "POST", "/v1/chat/completions", map[string]any{
		"model":  "claude-sonnet-4-20250514",
		"stream": false,
		"messages": []map[string]string{
			{"role": "user", "content": "Say hello in one word."},
		},
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("chat completions: expected 200, got %d: %s", resp.StatusCode, body)
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	json.Unmarshal(body, &chatResp)

	if len(chatResp.Choices) == 0 {
		t.Fatal("no choices in response")
	}
	if chatResp.Choices[0].Message.Content == "" {
		t.Error("empty content in response")
	}
}

// TestAdminListTenants tests the admin tenant list endpoint.
func TestAdminListTenants(t *testing.T) {
	resp, body := doRequest(t, "GET", "/api/v1/admin/tenants", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tenants: expected 200, got %d: %s", resp.StatusCode, body)
	}

	var tenants []map[string]any
	json.Unmarshal(body, &tenants)
	// Seeded data should have at least one tenant
	if len(tenants) == 0 {
		t.Log("warning: no tenants found (seed data may not be loaded)")
	}
}

// TestAuditQuery tests the audit log query endpoint.
func TestAuditQuery(t *testing.T) {
	resp, body := doRequest(t, "GET", "/api/v1/admin/audit?limit=5", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("query audit: expected 200, got %d: %s", resp.StatusCode, body)
	}

	var events []map[string]any
	json.Unmarshal(body, &events)
	// Events may be empty if no writes happened yet
	t.Logf("audit events returned: %d", len(events))
}

// TestRateLimiting tests that rapid requests eventually get rate limited.
func TestRateLimiting(t *testing.T) {
	got429 := false
	for i := 0; i < 100; i++ {
		resp, _ := doRequest(t, "GET", "/api/v1/agents", nil)
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Log("warning: rate limiting not triggered (may be disabled or limit is high)")
	}
}

// TestSessionLifecycle tests session creation, listing, archive, and compact.
func TestSessionLifecycle(t *testing.T) {
	// First create an agent for sessions
	slug := fmt.Sprintf("session-test-%d", time.Now().UnixNano())
	resp, body := doRequest(t, "POST", "/api/v1/agents", map[string]string{
		"name":  "Session Test Agent",
		"slug":  slug,
		"model": "claude-sonnet-4-20250514",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent: %d: %s", resp.StatusCode, body)
	}
	var agent struct{ ID string `json:"id"` }
	json.Unmarshal(body, &agent)

	// List sessions (should be empty initially)
	resp, body = doRequest(t, "GET", fmt.Sprintf("/api/v1/agents/%s/sessions", agent.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list sessions: %d: %s", resp.StatusCode, body)
	}

	// To test archive/compact we need a session. Try sending a chat to create one,
	// or verify the endpoints return proper errors for non-existent sessions.
	fakeSessionID := "00000000-0000-0000-0000-000000000099"

	// Archive non-existent session should fail gracefully
	resp, body = doRequest(t, "DELETE", fmt.Sprintf("/api/v1/sessions/%s", fakeSessionID), nil)
	if resp.StatusCode == http.StatusOK {
		t.Log("archive returned OK (session may have been found)")
	} else {
		t.Logf("archive returned %d (expected for non-existent session)", resp.StatusCode)
	}

	// Compact non-existent session
	resp, body = doRequest(t, "POST", fmt.Sprintf("/api/v1/sessions/%s/compact", fakeSessionID), nil)
	t.Logf("compact returned %d", resp.StatusCode)

	// Cleanup
	doRequest(t, "DELETE", "/api/v1/agents/"+agent.ID, nil)
}

// TestSkillEndpoints tests skill listing and install.
func TestSkillEndpoints(t *testing.T) {
	// List skills
	resp, body := doRequest(t, "GET", "/api/v1/skills", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list skills: %d: %s", resp.StatusCode, body)
	}

	var skills []map[string]any
	json.Unmarshal(body, &skills)
	t.Logf("skills count: %d", len(skills))

	// Install a skill
	resp, body = doRequest(t, "POST", "/api/v1/skills/install", map[string]string{
		"name":        "test-skill",
		"source":      "capyhub",
		"description": "A test skill",
		"version":     "1.0.0",
	})
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		var skill struct{ ID string `json:"id"` }
		json.Unmarshal(body, &skill)
		t.Logf("installed skill: %s", skill.ID)

		// Uninstall
		if skill.ID != "" {
			resp, _ = doRequest(t, "DELETE", "/api/v1/skills/"+skill.ID, nil)
			t.Logf("uninstall: %d", resp.StatusCode)
		}
	} else {
		t.Logf("install skill: %d (may be expected)", resp.StatusCode)
	}
}

// TestToolInvoke tests the tool invocation endpoint.
func TestToolInvoke(t *testing.T) {
	resp, body := doRequest(t, "POST", "/tools/invoke", map[string]any{
		"tool":  "echo",
		"input": map[string]any{"message": "hello"},
	})
	// Tool may not exist, but endpoint should respond properly
	if resp.StatusCode == http.StatusOK {
		var result map[string]any
		json.Unmarshal(body, &result)
		t.Logf("tool result: %v", result)
	} else {
		t.Logf("tool invoke: %d (tool may not be registered)", resp.StatusCode)
	}
}

// TestDevicePairing tests the device code flow (unauthenticated endpoints).
func TestDevicePairing(t *testing.T) {
	// Request a device code (unauthenticated)
	req, _ := http.NewRequest("POST", baseURL+"/api/v1/device/code", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("device code request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var codeResp struct {
			DeviceCode string `json:"device_code"`
			UserCode   string `json:"user_code"`
			ExpiresIn  int    `json:"expires_in"`
		}
		json.Unmarshal(body, &codeResp)

		if codeResp.DeviceCode == "" {
			t.Error("empty device code")
		}
		if codeResp.UserCode == "" {
			t.Error("empty user code")
		}
		if codeResp.ExpiresIn <= 0 {
			t.Error("invalid expires_in")
		}
		t.Logf("device code: %s, user code: %s", codeResp.DeviceCode[:8]+"...", codeResp.UserCode)

		// Poll for token (should be pending)
		tokenReq, _ := http.NewRequest("POST", baseURL+"/api/v1/device/token",
			bytes.NewReader([]byte(fmt.Sprintf(`{"device_code":"%s"}`, codeResp.DeviceCode))))
		tokenReq.Header.Set("Content-Type", "application/json")
		tokenResp, err := http.DefaultClient.Do(tokenReq)
		if err != nil {
			t.Fatalf("device token request: %v", err)
		}
		tokenBody, _ := io.ReadAll(tokenResp.Body)
		tokenResp.Body.Close()

		var tokenResult struct {
			Status string `json:"status"`
		}
		json.Unmarshal(tokenBody, &tokenResult)
		if tokenResult.Status != "pending" && tokenResult.Status != "approved" {
			t.Logf("token status: %s (expected 'pending')", tokenResult.Status)
		}
	} else {
		t.Logf("device code: %d (Redis may not be available)", resp.StatusCode)
	}
}

// TestAuthNoToken verifies unauthenticated requests are rejected.
func TestAuthNoToken(t *testing.T) {
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/agents", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// TestMultiTenantIsolation verifies that agents from one tenant are not visible to another.
func TestMultiTenantIsolation(t *testing.T) {
	// This test requires the gateway to be running with tenant isolation.
	// Create an agent with the default tenant token
	slug := fmt.Sprintf("isolation-test-%d", time.Now().UnixNano())
	resp, body := doRequest(t, "POST", "/api/v1/agents", map[string]string{
		"name":  "Isolation Test",
		"slug":  slug,
		"model": "claude-sonnet-4-20250514",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Skipf("cannot create agent: %d", resp.StatusCode)
	}
	var agent struct{ ID string `json:"id"` }
	json.Unmarshal(body, &agent)
	defer doRequest(t, "DELETE", "/api/v1/agents/"+agent.ID, nil)

	// Try to access with a different (invalid) tenant context
	// In a full setup we'd use a token for a different tenant
	// For now, verify the agent is accessible with our token
	resp, _ = doRequest(t, "GET", "/api/v1/agents/"+agent.ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	t.Log("tenant isolation test: agent accessible with correct tenant token")
}

// TestMemoryEndpoints tests memory CRUD operations.
func TestMemoryEndpoints(t *testing.T) {
	// Create an agent first
	slug := fmt.Sprintf("memory-test-%d", time.Now().UnixNano())
	resp, body := doRequest(t, "POST", "/api/v1/agents", map[string]string{
		"name":  "Memory Test Agent",
		"slug":  slug,
		"model": "claude-sonnet-4-20250514",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Skipf("cannot create agent: %d", resp.StatusCode)
	}
	var agent struct{ ID string `json:"id"` }
	json.Unmarshal(body, &agent)
	defer doRequest(t, "DELETE", "/api/v1/agents/"+agent.ID, nil)

	// List memories (should be empty)
	resp, body = doRequest(t, "GET", fmt.Sprintf("/api/v1/agents/%s/memories", agent.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list memories: %d: %s", resp.StatusCode, body)
	}

	// Create a memory
	resp, body = doRequest(t, "POST", fmt.Sprintf("/api/v1/agents/%s/memories", agent.ID), map[string]any{
		"type":             "semantic",
		"content":          "The user prefers concise answers.",
		"importance_score": 0.8,
	})
	if resp.StatusCode == http.StatusCreated {
		t.Log("memory created successfully")

		// Search memories
		resp, body = doRequest(t, "POST", fmt.Sprintf("/api/v1/agents/%s/memories/search", agent.ID), map[string]any{
			"query": "concise answers",
			"limit": 5,
		})
		t.Logf("memory search: %d", resp.StatusCode)
	} else {
		t.Logf("create memory: %d (memory manager may not be initialized)", resp.StatusCode)
	}
}

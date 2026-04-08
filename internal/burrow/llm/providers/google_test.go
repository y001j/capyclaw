package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"CapyClaw/internal/burrow/llm"
)

func TestGoogleClient_Provider(t *testing.T) {
	c := NewGoogleClient("test-key", "")
	if c.Provider() != "google" {
		t.Errorf("expected google, got %s", c.Provider())
	}
}

func TestGoogleClient_DefaultBaseURL(t *testing.T) {
	c := NewGoogleClient("key", "")
	if c.baseURL != "https://generativelanguage.googleapis.com" {
		t.Errorf("baseURL = %s", c.baseURL)
	}
}

func TestGoogleClient_CustomBaseURL(t *testing.T) {
	c := NewGoogleClient("key", "http://custom:8080")
	if c.baseURL != "http://custom:8080" {
		t.Errorf("baseURL = %s", c.baseURL)
	}
}

// --- parseGeminiEvent tests (core parsing logic) ---

func TestParseGeminiEvent_TextContent(t *testing.T) {
	client := NewGoogleClient("test-key", "")

	data := `{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello World"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
	chunks := client.parseGeminiEvent(&llm.SSEEvent{Data: data})

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks (text, usage, done), got %d", len(chunks))
	}

	if chunks[0].Type != "text" || chunks[0].Content != "Hello World" {
		t.Errorf("chunk[0] = type=%s content=%q", chunks[0].Type, chunks[0].Content)
	}

	// Find usage
	var usage *llm.Usage
	for _, c := range chunks {
		if c.Type == "usage" && c.Usage != nil {
			usage = c.Usage
		}
	}
	if usage == nil {
		t.Fatal("no usage chunk")
	}
	if usage.InputTokens != 10 {
		t.Errorf("input tokens = %d, want 10", usage.InputTokens)
	}
	if usage.OutputTokens != 5 {
		t.Errorf("output tokens = %d, want 5", usage.OutputTokens)
	}

	// Find done
	var doneFound bool
	for _, c := range chunks {
		if c.Type == "done" {
			doneFound = true
		}
	}
	if !doneFound {
		t.Error("no done chunk")
	}
}

func TestParseGeminiEvent_MultipleTextParts(t *testing.T) {
	client := NewGoogleClient("test-key", "")

	data := `{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello "},{"text":"World"}]},"finishReason":"STOP"}]}`
	chunks := client.parseGeminiEvent(&llm.SSEEvent{Data: data})

	textCount := 0
	for _, c := range chunks {
		if c.Type == "text" {
			textCount++
		}
	}
	if textCount != 2 {
		t.Errorf("expected 2 text chunks, got %d", textCount)
	}
}

func TestParseGeminiEvent_ToolCall(t *testing.T) {
	client := NewGoogleClient("test-key", "")

	data := `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"Tokyo","units":"celsius"}}}]},"finishReason":"STOP"}]}`
	chunks := client.parseGeminiEvent(&llm.SSEEvent{Data: data})

	var toolCall *llm.ToolCall
	for _, c := range chunks {
		if c.Type == "tool_call" && c.ToolCall != nil {
			toolCall = c.ToolCall
		}
	}
	if toolCall == nil {
		t.Fatal("no tool_call chunk")
	}
	if toolCall.Name != "get_weather" {
		t.Errorf("name = %s, want get_weather", toolCall.Name)
	}
	// ID should match Name for Gemini
	if toolCall.ID != "get_weather" {
		t.Errorf("id = %s, want get_weather", toolCall.ID)
	}
	// Verify args are in input
	if !strings.Contains(toolCall.Input, "Tokyo") {
		t.Errorf("input = %s, doesn't contain Tokyo", toolCall.Input)
	}
}

func TestParseGeminiEvent_NonStopFinish(t *testing.T) {
	client := NewGoogleClient("test-key", "")

	data := `{"candidates":[{"content":{"role":"model","parts":[{"text":""}]},"finishReason":"MAX_TOKENS"}]}`
	chunks := client.parseGeminiEvent(&llm.SSEEvent{Data: data})

	var found bool
	for _, c := range chunks {
		if c.FinishReason == "MAX_TOKENS" {
			found = true
		}
	}
	if !found {
		t.Error("expected MAX_TOKENS finish reason")
	}

	// STOP emits a "done" chunk, MAX_TOKENS does not
	for _, c := range chunks {
		if c.Type == "done" {
			t.Error("MAX_TOKENS should not emit done chunk")
		}
	}
}

func TestParseGeminiEvent_UsageOnly(t *testing.T) {
	client := NewGoogleClient("test-key", "")

	data := `{"candidates":[],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":50}}`
	chunks := client.parseGeminiEvent(&llm.SSEEvent{Data: data})

	var usage *llm.Usage
	for _, c := range chunks {
		if c.Type == "usage" {
			usage = c.Usage
		}
	}
	if usage == nil {
		t.Fatal("expected usage chunk")
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 50 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestParseGeminiEvent_InvalidJSON(t *testing.T) {
	client := NewGoogleClient("test-key", "")
	chunks := client.parseGeminiEvent(&llm.SSEEvent{Data: "not valid json"})
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for invalid JSON, got %d", len(chunks))
	}
}

func TestParseGeminiEvent_EmptyData(t *testing.T) {
	client := NewGoogleClient("test-key", "")
	chunks := client.parseGeminiEvent(&llm.SSEEvent{Data: ""})
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty data, got %d", len(chunks))
	}
}

// --- HTTP request construction tests ---

func TestGoogleClient_Stream_RequestFormat(t *testing.T) {
	var capturedBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)

		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %s", r.Header.Get("Content-Type"))
		}
		// Verify URL path includes model and api key
		if !strings.Contains(r.URL.Path, "gemini-2.0-flash") {
			t.Errorf("path = %s, missing model", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("key = %s", r.URL.Query().Get("key"))
		}

		w.WriteHeader(http.StatusBadRequest) // End quickly
	}))
	defer ts.Close()

	client := NewGoogleClient("test-key", ts.URL)
	client.Stream(context.Background(), &llm.Request{
		Model: "gemini-2.0-flash",
		Messages: []llm.Message{
			{Role: "system", Content: "You are helpful"},
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
			{Role: "user", Content: "How are you?"},
		},
		MaxTokens:   500,
		Temperature: 0.7,
		Tools: []llm.Tool{{
			Name:        "calc",
			Description: "Calculator",
			Parameters:  map[string]any{"expr": map[string]any{"type": "string"}},
		}},
	})

	var req geminiRequest
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	// System instruction
	if req.SystemInstruction == nil {
		t.Fatal("expected system instruction")
	}
	if req.SystemInstruction.Parts[0].Text != "You are helpful" {
		t.Errorf("system = %s", req.SystemInstruction.Parts[0].Text)
	}

	// Messages (system excluded from contents)
	if len(req.Contents) != 3 {
		t.Fatalf("expected 3 contents (no system), got %d", len(req.Contents))
	}
	if req.Contents[0].Role != "user" {
		t.Errorf("contents[0].role = %s, want user", req.Contents[0].Role)
	}
	if req.Contents[1].Role != "model" {
		t.Errorf("contents[1].role = %s, want model (mapped from assistant)", req.Contents[1].Role)
	}

	// Generation config
	if req.GenerationConfig.MaxOutputTokens != 500 {
		t.Errorf("max_tokens = %d", req.GenerationConfig.MaxOutputTokens)
	}
	if req.GenerationConfig.Temperature == nil || *req.GenerationConfig.Temperature != 0.7 {
		t.Errorf("temperature = %v", req.GenerationConfig.Temperature)
	}

	// Tools
	if len(req.Tools) != 1 || len(req.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("tools = %+v", req.Tools)
	}
	if req.Tools[0].FunctionDeclarations[0].Name != "calc" {
		t.Errorf("tool name = %s", req.Tools[0].FunctionDeclarations[0].Name)
	}
}

func TestGoogleClient_Stream_DefaultMaxTokens(t *testing.T) {
	var capturedBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	client := NewGoogleClient("test-key", ts.URL)
	client.Stream(context.Background(), &llm.Request{
		Model:    "gemini-2.0-flash",
		Messages: []llm.Message{{Role: "user", Content: "Hi"}},
	})

	var req geminiRequest
	json.Unmarshal(capturedBody, &req)

	if req.GenerationConfig.MaxOutputTokens != 4096 {
		t.Errorf("default max_tokens = %d, want 4096", req.GenerationConfig.MaxOutputTokens)
	}
}

func TestGoogleClient_Stream_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid request"}`))
	}))
	defer ts.Close()

	client := NewGoogleClient("test-key", ts.URL)
	_, err := client.Stream(context.Background(), &llm.Request{
		Model:    "gemini-2.0-flash",
		Messages: []llm.Message{{Role: "user", Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %v, should contain status code", err)
	}
}

// --- CountTokens tests ---

func TestGoogleClient_CountTokens(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify role mapping
		var body struct {
			Contents []struct {
				Role string `json:"role"`
			} `json:"contents"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		for _, c := range body.Contents {
			if c.Role == "assistant" {
				t.Error("assistant should be mapped to model")
			}
			if c.Role == "system" {
				t.Error("system should be mapped to user for countTokens")
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"totalTokens": 42})
	}))
	defer ts.Close()

	client := NewGoogleClient("test-key", ts.URL)
	count, err := client.CountTokens(context.Background(), []llm.Message{
		{Role: "system", Content: "System msg"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 42 {
		t.Errorf("count = %d, want 42", count)
	}
}

func TestGoogleClient_CountTokens_Fallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	msg := "Hello world test message" // len=24, 24/4=6
	client := NewGoogleClient("test-key", ts.URL)
	count, err := client.CountTokens(context.Background(), []llm.Message{
		{Role: "user", Content: msg},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := len(msg) / 4
	if count != expected {
		t.Errorf("count = %d, want %d", count, expected)
	}
}

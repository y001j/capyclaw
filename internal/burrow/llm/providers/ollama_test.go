package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"CapyClaw/internal/burrow/llm"
)

func TestOllamaClient_Provider(t *testing.T) {
	c := NewOllamaClient("")
	if c.Provider() != "ollama" {
		t.Errorf("expected ollama, got %s", c.Provider())
	}
}

func TestOllamaClient_DefaultBaseURL(t *testing.T) {
	c := NewOllamaClient("")
	if c.baseURL != "http://localhost:11434" {
		t.Errorf("baseURL = %s, want http://localhost:11434", c.baseURL)
	}
}

func TestOllamaClient_Stream_TextResponse(t *testing.T) {
	clientDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected /api/chat, got %s", r.URL.Path)
		}

		var req ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		if req.Model != "llama3" {
			t.Errorf("model = %s, want llama3", req.Model)
		}
		if !req.Stream {
			t.Error("expected stream=true")
		}

		flusher := w.(http.Flusher)

		// Chunk 1
		chunk1 := ollamaStreamChunk{
			Message: ollamaMessage{Role: "assistant", Content: "Hello "},
		}
		data1, _ := json.Marshal(chunk1)
		fmt.Fprintf(w, "%s\n", data1)
		flusher.Flush()

		// Chunk 2
		chunk2 := ollamaStreamChunk{
			Message: ollamaMessage{Role: "assistant", Content: "World!"},
		}
		data2, _ := json.Marshal(chunk2)
		fmt.Fprintf(w, "%s\n", data2)
		flusher.Flush()

		// Final chunk
		chunk3 := ollamaStreamChunk{
			Message:         ollamaMessage{Role: "assistant"},
			Done:            true,
			PromptEvalCount: 15,
			EvalCount:       8,
		}
		data3, _ := json.Marshal(chunk3)
		fmt.Fprintf(w, "%s\n", data3)
		flusher.Flush()

		select {
		case <-clientDone:
		case <-time.After(5 * time.Second):
		}
	}))
	defer ts.Close()

	client := NewOllamaClient(ts.URL)
	ch, err := client.Stream(context.Background(), &llm.Request{
		Model:     "llama3",
		Messages:  []llm.Message{{Role: "user", Content: "Hello"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []*llm.Chunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	close(clientDone)

	// Expect: text("Hello "), text("World!"), usage, done
	if len(chunks) < 4 {
		t.Fatalf("expected at least 4 chunks, got %d", len(chunks))
	}

	if chunks[0].Type != "text" || chunks[0].Content != "Hello " {
		t.Errorf("chunk[0] = type=%s content=%q", chunks[0].Type, chunks[0].Content)
	}
	if chunks[1].Type != "text" || chunks[1].Content != "World!" {
		t.Errorf("chunk[1] = type=%s content=%q", chunks[1].Type, chunks[1].Content)
	}

	var usageFound, doneFound bool
	for _, c := range chunks {
		if c.Type == "usage" && c.Usage != nil {
			usageFound = true
			if c.Usage.InputTokens != 15 {
				t.Errorf("input tokens = %d, want 15", c.Usage.InputTokens)
			}
			if c.Usage.OutputTokens != 8 {
				t.Errorf("output tokens = %d, want 8", c.Usage.OutputTokens)
			}
		}
		if c.Type == "done" {
			doneFound = true
		}
	}
	if !usageFound {
		t.Error("no usage chunk")
	}
	if !doneFound {
		t.Error("no done chunk")
	}
}

func TestOllamaClient_Stream_ToolCall(t *testing.T) {
	clientDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)

		chunk := ollamaStreamChunk{
			Message: ollamaMessage{
				Role: "assistant",
				ToolCalls: []ollamaToolCall{{
					Function: struct {
						Name      string         `json:"name"`
						Arguments map[string]any `json:"arguments"`
					}{
						Name:      "search",
						Arguments: map[string]any{"query": "capybara"},
					},
				}},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "%s\n", data)
		flusher.Flush()

		done := ollamaStreamChunk{
			Message: ollamaMessage{Role: "assistant"},
			Done:    true,
		}
		doneData, _ := json.Marshal(done)
		fmt.Fprintf(w, "%s\n", doneData)
		flusher.Flush()

		select {
		case <-clientDone:
		case <-time.After(5 * time.Second):
		}
	}))
	defer ts.Close()

	client := NewOllamaClient(ts.URL)
	ch, err := client.Stream(context.Background(), &llm.Request{
		Model:    "llama3",
		Messages: []llm.Message{{Role: "user", Content: "Search for capybara"}},
		Tools: []llm.Tool{{
			Name:        "search",
			Description: "Search the web",
			Parameters:  map[string]any{"query": map[string]any{"type": "string"}},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var toolCallFound bool
	for chunk := range ch {
		if chunk.Type == "tool_call" && chunk.ToolCall != nil {
			toolCallFound = true
			if chunk.ToolCall.Name != "search" {
				t.Errorf("tool name = %s, want search", chunk.ToolCall.Name)
			}
		}
	}
	close(clientDone)

	if !toolCallFound {
		t.Error("no tool_call chunk")
	}
}

func TestOllamaClient_Stream_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	client := NewOllamaClient(ts.URL)
	_, err := client.Stream(context.Background(), &llm.Request{
		Model:    "llama3",
		Messages: []llm.Message{{Role: "user", Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestOllamaClient_Stream_Options(t *testing.T) {
	clientDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Options.NumPredict != 200 {
			t.Errorf("num_predict = %d, want 200", req.Options.NumPredict)
		}
		if req.Options.Temperature == nil || *req.Options.Temperature != 0.7 {
			t.Errorf("temperature = %v, want 0.7", req.Options.Temperature)
		}

		chunk := ollamaStreamChunk{Done: true}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "%s\n", data)
		w.(http.Flusher).Flush()

		select {
		case <-clientDone:
		case <-time.After(5 * time.Second):
		}
	}))
	defer ts.Close()

	client := NewOllamaClient(ts.URL)
	ch, err := client.Stream(context.Background(), &llm.Request{
		Model:       "llama3",
		Messages:    []llm.Message{{Role: "user", Content: "Hi"}},
		MaxTokens:   200,
		Temperature: 0.7,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for range ch {
	}
	close(clientDone)
}

func TestOllamaClient_CountTokens(t *testing.T) {
	client := NewOllamaClient("")
	msg := "Hello world test message here" // len=29, 29/4=7
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

func TestOllamaClient_Stream_ContextCancel(t *testing.T) {
	started := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Must write headers/body to unblock client.Do()
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewOllamaClient(ts.URL)
	ch, err := client.Stream(ctx, &llm.Request{
		Model:    "llama3",
		Messages: []llm.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	<-started
	cancel()
	for range ch {
	}
}

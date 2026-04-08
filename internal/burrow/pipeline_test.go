package burrow

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"CapyClaw/internal/burrow/llm"
	"CapyClaw/internal/shared/config"
)

// mockLLMClient is a simple mock for testing the pipeline.
type mockLLMClient struct {
	chunks []*llm.Chunk
}

func (m *mockLLMClient) Stream(ctx context.Context, req *llm.Request) (<-chan *llm.Chunk, error) {
	ch := make(chan *llm.Chunk, len(m.chunks))
	for _, c := range m.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *mockLLMClient) CountTokens(ctx context.Context, messages []llm.Message) (int, error) {
	return 100, nil
}

func (m *mockLLMClient) Provider() string {
	return "mock"
}

func TestPipeline_Execute_WithMockLLM(t *testing.T) {
	mock := &mockLLMClient{
		chunks: []*llm.Chunk{
			{Type: "text", Content: "Hello, "},
			{Type: "text", Content: "world!"},
			{Type: "done", Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5}},
		},
	}

	cfg := &config.BurrowConfig{DefaultModel: "test-model"}
	pipeline := NewPipeline(&PipelineDeps{
		LLMClient: mock,
		Config:    cfg,
	})

	req := &PipelineRequest{
		AgentID:  uuid.New(),
		TenantID: uuid.New(),
		Message: &IncomingMessage{
			SessionKey: "test-session",
			Content:    "Hi",
			Role:       "user",
		},
		AgentConfig: AgentConfig{Model: "test-model"},
	}

	outputCh, err := pipeline.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var content string
	var gotDone bool
	for chunk := range outputCh {
		switch chunk.Type {
		case "text":
			content += chunk.Content
		case "done":
			gotDone = true
		case "error":
			t.Errorf("unexpected error chunk: %s", chunk.Error)
		}
	}

	if content != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", content)
	}
	if !gotDone {
		t.Error("expected done chunk")
	}
}

func TestPipeline_Execute_NoLLMClient(t *testing.T) {
	cfg := &config.BurrowConfig{DefaultModel: "test-model"}
	pipeline := NewPipeline(&PipelineDeps{
		LLMClient: nil,
		Config:    cfg,
	})

	req := &PipelineRequest{
		AgentID:  uuid.New(),
		TenantID: uuid.New(),
		Message: &IncomingMessage{
			SessionKey: "test-session",
			Content:    "Hi",
			Role:       "user",
		},
		AgentConfig: AgentConfig{Model: "test-model"},
	}

	outputCh, err := pipeline.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotError bool
	for chunk := range outputCh {
		if chunk.Type == "error" {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected error chunk when no LLM client")
	}
}

func TestModelSelector(t *testing.T) {
	ms := &ModelSelector{defaultModel: "claude-default"}

	// With agent model
	req := &PipelineRequest{AgentConfig: AgentConfig{Model: "gpt-4"}}
	result, err := ms.Process(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "gpt-4" {
		t.Errorf("expected gpt-4, got %s", result.Model)
	}

	// Without agent model (use default)
	req2 := &PipelineRequest{AgentConfig: AgentConfig{}}
	result2, err := ms.Process(context.Background(), req2)
	if err != nil {
		t.Fatal(err)
	}
	if result2.Model != "claude-default" {
		t.Errorf("expected claude-default, got %s", result2.Model)
	}
}

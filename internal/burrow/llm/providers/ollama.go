package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"CapyClaw/internal/burrow/llm"
)

// OllamaClient implements the LLM client for local Ollama instances.
type OllamaClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewOllamaClient creates a new Ollama client.
func NewOllamaClient(baseURL string) *OllamaClient {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaClient{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (c *OllamaClient) Provider() string { return "ollama" }

func (c *OllamaClient) Stream(ctx context.Context, req *llm.Request) (<-chan *llm.Chunk, error) {
	// Build Ollama chat request
	body := ollamaRequest{
		Model:  req.Model,
		Stream: true,
	}

	// Convert messages
	for _, msg := range req.Messages {
		body.Messages = append(body.Messages, ollamaMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Options
	if req.MaxTokens > 0 {
		body.Options.NumPredict = req.MaxTokens
	}
	if req.Temperature > 0 {
		body.Options.Temperature = &req.Temperature
	}

	// Convert tools (Ollama 0.4+ supports function calling)
	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, ollamaTool{
				Type: "function",
				Function: ollamaFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters: map[string]any{
						"type":       "object",
						"properties": t.Parameters,
					},
				},
			})
		}
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan *llm.Chunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		// Increase buffer size for large responses
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var chunk ollamaStreamChunk
			if err := json.Unmarshal(line, &chunk); err != nil {
				continue
			}

			// Text content
			if chunk.Message.Content != "" {
				ch <- &llm.Chunk{
					Type:    "text",
					Content: chunk.Message.Content,
				}
			}

			// Tool calls
			for _, tc := range chunk.Message.ToolCalls {
				inputJSON, _ := json.Marshal(tc.Function.Arguments)
				ch <- &llm.Chunk{
					Type: "tool_call",
					ToolCall: &llm.ToolCall{
						ID:    tc.Function.Name,
						Name:  tc.Function.Name,
						Input: string(inputJSON),
					},
				}
			}

			// Final chunk
			if chunk.Done {
				ch <- &llm.Chunk{
					Type: "usage",
					Usage: &llm.Usage{
						InputTokens:  chunk.PromptEvalCount,
						OutputTokens: chunk.EvalCount,
					},
				}
				ch <- &llm.Chunk{Type: "done"}
				return
			}
		}
	}()

	return ch, nil
}

func (c *OllamaClient) CountTokens(ctx context.Context, messages []llm.Message) (int, error) {
	// Ollama doesn't have a dedicated token counting endpoint.
	// Rough estimation: ~4 chars per token.
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
	}
	return total, nil
}

// --- Ollama API types ---

type ollamaRequest struct {
	Model    string           `json:"model"`
	Messages []ollamaMessage  `json:"messages"`
	Stream   bool             `json:"stream"`
	Options  ollamaOptions    `json:"options,omitempty"`
	Tools    []ollamaTool     `json:"tools,omitempty"`
}

type ollamaMessage struct {
	Role      string             `json:"role"`
	Content   string             `json:"content"`
	ToolCalls []ollamaToolCall   `json:"tool_calls,omitempty"`
}

type ollamaOptions struct {
	NumPredict  int      `json:"num_predict,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

type ollamaTool struct {
	Type     string         `json:"type"`
	Function ollamaFunction `json:"function"`
}

type ollamaFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type ollamaStreamChunk struct {
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	PromptEvalCount int           `json:"prompt_eval_count,omitempty"`
	EvalCount       int           `json:"eval_count,omitempty"`
}

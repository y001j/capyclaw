package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"CapyClaw/internal/burrow/llm"
)

// OpenAIClient implements the LLM client for OpenAI's API.
type OpenAIClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewOpenAIClient creates a new OpenAI API client.
func NewOpenAIClient(apiKey, baseURL string) *OpenAIClient {
	// Normalize: strip trailing /v1 so we can append /v1/chat/completions uniformly
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	return &OpenAIClient{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (c *OpenAIClient) Provider() string { return "openai" }

func (c *OpenAIClient) Stream(ctx context.Context, req *llm.Request) (<-chan *llm.Chunk, error) {
	body := map[string]any{
		"model":  req.Model,
		"stream": true,
	}

	// Convert messages with tool_call and tool_result support
	var messages []any
	for _, msg := range req.Messages {
		// Assistant messages with tool_calls
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			toolCalls := make([]map[string]any, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				toolCalls[i] = map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": tc.Input,
					},
				}
			}
			m := map[string]any{"role": "assistant", "tool_calls": toolCalls}
			if msg.Content != "" {
				m["content"] = msg.Content
			}
			messages = append(messages, m)
			continue
		}

		// Tool result messages
		if msg.Role == "tool" && len(msg.ToolResults) > 0 {
			for _, tr := range msg.ToolResults {
				messages = append(messages, map[string]any{
					"role":         "tool",
					"tool_call_id": tr.ToolUseID,
					"content":      tr.Content,
				})
			}
			continue
		}

		messages = append(messages, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}
	body["messages"] = messages

	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.Tools) > 0 {
		body["tools"] = convertToolsToOpenAI(req.Tools)
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan *llm.Chunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		parser := llm.NewSSEParser(resp.Body)
		accumulator := llm.NewOpenAIStreamAccumulator()

		for event := range parser.Parse(ctx) {
			if event.Data == "[DONE]" {
				ch <- &llm.Chunk{Type: "done"}
				return
			}

			chunks, err := accumulator.ParseChunk(event.Data)
			if err != nil {
				continue
			}
			for _, chunk := range chunks {
				ch <- chunk
			}
		}
	}()

	return ch, nil
}

func (c *OpenAIClient) CountTokens(ctx context.Context, messages []llm.Message) (int, error) {
	// Rough estimation: ~4 chars per token
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
	}
	return total, nil
}

func convertToolsToOpenAI(tools []llm.Tool) []map[string]any {
	result := make([]map[string]any, len(tools))
	for i, t := range tools {
		result[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			},
		}
	}
	return result
}

package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"CapyClaw/internal/burrow/llm"
)

// VolcEngineClient implements the LLM client for Volcengine Ark API (火山引擎方舟).
// The Ark API is OpenAI-compatible with minor differences.
type VolcEngineClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewVolcEngineClient creates a new Volcengine Ark API client.
func NewVolcEngineClient(apiKey, baseURL string) *VolcEngineClient {
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/v3")
	return &VolcEngineClient{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (c *VolcEngineClient) Provider() string { return "volcengine" }

func (c *VolcEngineClient) Stream(ctx context.Context, req *llm.Request) (<-chan *llm.Chunk, error) {
	body := map[string]any{
		"model":  req.Model,
		"stream": true,
		// Request usage in the final streaming chunk
		"stream_options": map[string]any{"include_usage": true},
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

	slog.Debug("volcengine request", "tools_count", len(req.Tools), "body_size", len(bodyJSON))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v3/chat/completions", bytes.NewReader(bodyJSON))
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
		return nil, fmt.Errorf("volcengine API error (status %d): %s", resp.StatusCode, string(errBody))
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

func (c *VolcEngineClient) CountTokens(ctx context.Context, messages []llm.Message) (int, error) {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
	}
	return total, nil
}

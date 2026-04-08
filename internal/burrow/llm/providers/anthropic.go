package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"CapyClaw/internal/burrow/llm"
)

// AnthropicClient implements the LLM client for Anthropic's Claude API.
type AnthropicClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewAnthropicClient creates a new Anthropic API client.
func NewAnthropicClient(apiKey, baseURL string) *AnthropicClient {
	return &AnthropicClient{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (c *AnthropicClient) Provider() string { return "anthropic" }

func (c *AnthropicClient) Stream(ctx context.Context, req *llm.Request) (<-chan *llm.Chunk, error) {
	// Build Anthropic Messages API request
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     true,
	}

	if req.MaxTokens <= 0 {
		body["max_tokens"] = 4096
	}

	// Convert messages: separate system from conversation
	var systemContent string
	var messages []map[string]any
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemContent = msg.Content
			continue
		}

		// Handle assistant messages with tool_use blocks
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			content := []map[string]any{}
			if msg.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				var inputObj any
				if json.Unmarshal([]byte(tc.Input), &inputObj) != nil {
					inputObj = map[string]any{}
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": inputObj,
				})
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": content})
			continue
		}

		// Handle tool_result messages (role="tool" with ToolResults)
		if msg.Role == "tool" && len(msg.ToolResults) > 0 {
			content := []map[string]any{}
			for _, tr := range msg.ToolResults {
				content = append(content, map[string]any{
					"type":        "tool_result",
					"tool_use_id": tr.ToolUseID,
					"content":     tr.Content,
				})
			}
			messages = append(messages, map[string]any{"role": "user", "content": content})
			continue
		}

		messages = append(messages, map[string]any{"role": msg.Role, "content": msg.Content})
	}
	body["messages"] = messages
	if systemContent != "" {
		body["system"] = systemContent
	}

	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}

	if len(req.Tools) > 0 {
		body["tools"] = convertToolsToAnthropic(req.Tools)
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	slog.Debug("anthropic request", "tools_count", len(req.Tools), "body_size", len(bodyJSON))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan *llm.Chunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		parser := llm.NewSSEParser(resp.Body)
		var currentToolID, currentToolName, currentToolInput string

		for event := range parser.Parse(ctx) {
			chunk := c.parseAnthropicEvent(event, &currentToolID, &currentToolName, &currentToolInput)
			if chunk != nil {
				ch <- chunk
			}
		}
	}()

	return ch, nil
}

func (c *AnthropicClient) parseAnthropicEvent(event *llm.SSEEvent, toolID, toolName, toolInput *string) *llm.Chunk {
	switch event.Event {
	case "message_start":
		// Extract input token count
		var data struct {
			Message struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(event.Data), &data) == nil && data.Message.Usage.InputTokens > 0 {
			return &llm.Chunk{
				Type:  "usage",
				Usage: &llm.Usage{InputTokens: data.Message.Usage.InputTokens},
			}
		}
		return nil

	case "content_block_start":
		var data struct {
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
		}
		if json.Unmarshal([]byte(event.Data), &data) == nil {
			if data.ContentBlock.Type == "tool_use" {
				*toolID = data.ContentBlock.ID
				*toolName = data.ContentBlock.Name
				*toolInput = ""
			}
		}
		return nil

	case "content_block_delta":
		var data struct {
			Delta struct {
				Type           string `json:"type"`
				Text           string `json:"text"`
				PartialJSON    string `json:"partial_json"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(event.Data), &data) == nil {
			switch data.Delta.Type {
			case "text_delta":
				return &llm.Chunk{Type: "text", Content: data.Delta.Text}
			case "input_json_delta":
				*toolInput += data.Delta.PartialJSON
			}
		}
		return nil

	case "content_block_stop":
		if *toolID != "" {
			chunk := &llm.Chunk{
				Type: "tool_call",
				ToolCall: &llm.ToolCall{
					ID:    *toolID,
					Name:  *toolName,
					Input: *toolInput,
				},
			}
			*toolID = ""
			*toolName = ""
			*toolInput = ""
			return chunk
		}
		return nil

	case "message_delta":
		var data struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(event.Data), &data) == nil {
			return &llm.Chunk{
				Type:         "text",
				FinishReason: data.Delta.StopReason,
				Usage:        &llm.Usage{OutputTokens: data.Usage.OutputTokens},
			}
		}
		return nil

	case "message_stop":
		return &llm.Chunk{Type: "done"}

	case "error":
		return &llm.Chunk{Type: "error", Content: event.Data}
	}

	return nil
}

func (c *AnthropicClient) CountTokens(ctx context.Context, messages []llm.Message) (int, error) {
	// Rough estimation: ~4 chars per token
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
	}
	return total, nil
}

func convertToolsToAnthropic(tools []llm.Tool) []map[string]any {
	result := make([]map[string]any, len(tools))
	for i, t := range tools {
		// If Parameters already has a "type" key, treat it as a full JSON schema
		inputSchema := t.Parameters
		if inputSchema == nil {
			inputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
		} else if _, hasType := inputSchema["type"]; !hasType {
			// Legacy format: wrap in object schema
			inputSchema = map[string]any{
				"type":       "object",
				"properties": inputSchema,
			}
		}
		result[i] = map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": inputSchema,
		}
	}
	return result
}

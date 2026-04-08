package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SSEParser parses Server-Sent Events from an LLM streaming response.
type SSEParser struct {
	reader *bufio.Reader
}

// NewSSEParser creates a new SSE parser from a reader.
func NewSSEParser(r io.Reader) *SSEParser {
	return &SSEParser{
		reader: bufio.NewReader(r),
	}
}

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
}

// Parse reads and parses SSE events, sending them to the returned channel.
func (p *SSEParser) Parse(ctx context.Context) <-chan *SSEEvent {
	ch := make(chan *SSEEvent, 64)

	go func() {
		defer close(ch)

		var event SSEEvent
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line, err := p.reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					ch <- &SSEEvent{Event: "error", Data: err.Error()}
				}
				return
			}

			line = strings.TrimRight(line, "\r\n")

			if line == "" {
				if event.Data != "" {
					e := event // copy before sending to avoid pointer aliasing
					ch <- &e
					event = SSEEvent{}
				}
				continue
			}

			if strings.HasPrefix(line, "data: ") {
				event.Data = strings.TrimPrefix(line, "data: ")
			} else if strings.HasPrefix(line, "event: ") {
				event.Event = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "id: ") {
				event.ID = strings.TrimPrefix(line, "id: ")
			}
		}
	}()

	return ch
}

// OpenAIToolCallDelta holds partial tool_call data from a streaming delta.
type OpenAIToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

// OpenAIStreamAccumulator accumulates tool_calls across streaming chunks.
type OpenAIStreamAccumulator struct {
	toolCalls map[int]*ToolCall // index → accumulated tool call
}

// NewOpenAIStreamAccumulator creates a new accumulator.
func NewOpenAIStreamAccumulator() *OpenAIStreamAccumulator {
	return &OpenAIStreamAccumulator{
		toolCalls: make(map[int]*ToolCall),
	}
}

// ParseChunk parses an OpenAI-format SSE data payload with tool_call accumulation.
func (a *OpenAIStreamAccumulator) ParseChunk(data string) ([]*Chunk, error) {
	if data == "[DONE]" {
		return []*Chunk{{Type: "done"}}, nil
	}

	var raw struct {
		Choices []struct {
			Delta struct {
				Content   string                `json:"content"`
				ToolCalls []OpenAIToolCallDelta  `json:"tool_calls,omitempty"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *Usage `json:"usage,omitempty"`
	}

	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return nil, fmt.Errorf("parsing chunk: %w", err)
	}

	// Handle usage-only chunk (火山引擎 stream_options.include_usage)
	if len(raw.Choices) == 0 && raw.Usage != nil {
		return []*Chunk{{Type: "usage", Usage: raw.Usage}}, nil
	}

	var chunks []*Chunk

	if len(raw.Choices) > 0 {
		choice := raw.Choices[0]

		// Text content
		if choice.Delta.Content != "" {
			chunks = append(chunks, &Chunk{Type: "text", Content: choice.Delta.Content})
		}

		// Accumulate tool_calls
		for _, tc := range choice.Delta.ToolCalls {
			existing, ok := a.toolCalls[tc.Index]
			if !ok {
				existing = &ToolCall{ID: tc.ID, Name: tc.Function.Name}
				a.toolCalls[tc.Index] = existing
			}
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Function.Name != "" {
				existing.Name = tc.Function.Name
			}
			existing.Input += tc.Function.Arguments
		}

		// On finish_reason, emit accumulated tool_calls
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			for _, tc := range a.toolCalls {
				chunks = append(chunks, &Chunk{
					Type:     "tool_call",
					ToolCall: &ToolCall{ID: tc.ID, Name: tc.Name, Input: tc.Input},
				})
			}
			// Clear for next turn
			a.toolCalls = make(map[int]*ToolCall)

			chunks = append(chunks, &Chunk{
				Type:         "text",
				FinishReason: *choice.FinishReason,
				Usage:        raw.Usage,
			})
		}
	}

	return chunks, nil
}

// ParseOpenAIChunk parses an OpenAI-format SSE data payload (simple, no tool_call support).
func ParseOpenAIChunk(data string) (*Chunk, error) {
	if data == "[DONE]" {
		return &Chunk{Type: "done"}, nil
	}

	var raw struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *Usage `json:"usage,omitempty"`
	}

	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return nil, fmt.Errorf("parsing chunk: %w", err)
	}

	chunk := &Chunk{Type: "text", Usage: raw.Usage}
	if len(raw.Choices) > 0 {
		chunk.Content = raw.Choices[0].Delta.Content
		chunk.FinishReason = raw.Choices[0].FinishReason
	}

	return chunk, nil
}

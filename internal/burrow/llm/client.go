package llm

import "context"

// Client is the unified interface for LLM provider communication.
type Client interface {
	// Stream sends a request and returns a channel of streaming response chunks.
	Stream(ctx context.Context, req *Request) (<-chan *Chunk, error)

	// CountTokens estimates the token count for the given messages.
	CountTokens(ctx context.Context, messages []Message) (int, error)

	// Provider returns the provider name.
	Provider() string
}

// Request represents an LLM API request.
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Tools       []Tool    `json:"tools,omitempty"`
	Stream      bool      `json:"stream"`
}

// Message represents a conversation message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`

	// For assistant messages: one or more tool_use blocks
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// For user messages: one or more tool_result blocks
	ToolResults []ToolResult `json:"tool_results,omitempty"`
}

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// Tool represents a tool definition sent to the LLM.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Chunk is a piece of streaming response from the LLM.
type Chunk struct {
	Type       string     `json:"type"` // text, tool_call, done, error
	Content    string     `json:"content,omitempty"`
	ToolCall   *ToolCall  `json:"tool_call,omitempty"`
	Usage      *Usage     `json:"usage,omitempty"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

// ToolCall represents a tool invocation from the LLM.
type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

// Usage contains token usage information.
type Usage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

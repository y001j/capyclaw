package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"CapyClaw/internal/burrow/llm"
)

// GoogleClient implements the LLM client for Google's Gemini API.
type GoogleClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewGoogleClient creates a new Google AI client.
func NewGoogleClient(apiKey, baseURL string) *GoogleClient {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	return &GoogleClient{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (c *GoogleClient) Provider() string { return "google" }

func (c *GoogleClient) Stream(ctx context.Context, req *llm.Request) (<-chan *llm.Chunk, error) {
	// Build Gemini request body
	body := geminiRequest{
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens: req.MaxTokens,
		},
	}
	if body.GenerationConfig.MaxOutputTokens <= 0 {
		body.GenerationConfig.MaxOutputTokens = 4096
	}
	if req.Temperature > 0 {
		body.GenerationConfig.Temperature = &req.Temperature
	}

	// Convert messages to Gemini format
	var systemInstruction string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemInstruction = msg.Content
			continue
		}
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}
		body.Contents = append(body.Contents, geminiContent{
			Role: role,
			Parts: []geminiPart{
				{Text: msg.Content},
			},
		})
	}
	if systemInstruction != "" {
		body.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: systemInstruction}},
		}
	}

	// Convert tools
	if len(req.Tools) > 0 {
		var decls []geminiFunctionDecl
		for _, t := range req.Tools {
			decls = append(decls, geminiFunctionDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters: map[string]any{
					"type":       "object",
					"properties": t.Parameters,
				},
			})
		}
		body.Tools = []geminiToolConfig{{FunctionDeclarations: decls}}
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	// Gemini streaming endpoint
	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s",
		c.baseURL, req.Model, c.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
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
		return nil, fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan *llm.Chunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		parser := llm.NewSSEParser(resp.Body)
		for event := range parser.Parse(ctx) {
			if event.Event == "error" {
				ch <- &llm.Chunk{Type: "error", Content: event.Data}
				return
			}

			chunks := c.parseGeminiEvent(event)
			for _, chunk := range chunks {
				ch <- chunk
			}
		}
	}()

	return ch, nil
}

func (c *GoogleClient) parseGeminiEvent(event *llm.SSEEvent) []*llm.Chunk {
	var resp geminiStreamResponse
	if err := json.Unmarshal([]byte(event.Data), &resp); err != nil {
		return nil
	}

	var chunks []*llm.Chunk

	for _, candidate := range resp.Candidates {
		for _, part := range candidate.Content.Parts {
			// Text content
			if part.Text != "" {
				chunks = append(chunks, &llm.Chunk{
					Type:    "text",
					Content: part.Text,
				})
			}
			// Function call
			if part.FunctionCall != nil {
				inputJSON, _ := json.Marshal(part.FunctionCall.Args)
				chunks = append(chunks, &llm.Chunk{
					Type: "tool_call",
					ToolCall: &llm.ToolCall{
						ID:    part.FunctionCall.Name, // Gemini doesn't have call IDs
						Name:  part.FunctionCall.Name,
						Input: string(inputJSON),
					},
				})
			}
		}

		// Check finish reason
		if candidate.FinishReason != "" && candidate.FinishReason != "STOP" {
			chunks = append(chunks, &llm.Chunk{
				Type:         "text",
				FinishReason: candidate.FinishReason,
			})
		}
	}

	// Usage metadata
	if resp.UsageMetadata != nil {
		chunks = append(chunks, &llm.Chunk{
			Type: "usage",
			Usage: &llm.Usage{
				InputTokens:  resp.UsageMetadata.PromptTokenCount,
				OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
			},
		})
	}

	// If this is the last chunk (has finish reason STOP), emit done
	for _, candidate := range resp.Candidates {
		if candidate.FinishReason == "STOP" {
			chunks = append(chunks, &llm.Chunk{Type: "done"})
			break
		}
	}

	return chunks
}

func (c *GoogleClient) CountTokens(ctx context.Context, messages []llm.Message) (int, error) {
	// Build count tokens request
	var contents []geminiContent
	for _, msg := range messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}
		if role == "system" {
			role = "user" // Gemini countTokens doesn't support system role directly
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.Content}},
		})
	}

	body := map[string]any{"contents": contents}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("marshaling count request: %w", err)
	}

	// Use the first model in the request, default to gemini-2.0-flash
	model := "gemini-2.0-flash"
	url := fmt.Sprintf("%s/v1beta/models/%s:countTokens?key=%s", c.baseURL, model, c.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return 0, fmt.Errorf("creating count request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("executing count request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Fall back to estimation
		total := 0
		for _, m := range messages {
			total += len(m.Content) / 4
		}
		return total, nil
	}

	var result struct {
		TotalTokens int `json:"totalTokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decoding count response: %w", err)
	}

	return result.TotalTokens, nil
}

// --- Gemini API types ---

type geminiRequest struct {
	Contents          []geminiContent    `json:"contents"`
	SystemInstruction *geminiContent     `json:"systemInstruction,omitempty"`
	GenerationConfig  geminiGenerationConfig `json:"generationConfig"`
	Tools             []geminiToolConfig `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text         string              `json:"text,omitempty"`
	FunctionCall *geminiFunctionCall `json:"functionCall,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens"`
	Temperature     *float64 `json:"temperature,omitempty"`
}

type geminiToolConfig struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations"`
}

type geminiFunctionDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type geminiStreamResponse struct {
	Candidates    []geminiCandidate    `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

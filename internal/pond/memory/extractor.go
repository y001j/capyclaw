package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"CapyClaw/internal/burrow/llm"
)

// Extractor extracts memories from conversation turns using an LLM.
type Extractor struct {
	llmClient llm.Client
	model     string
	manager   *Manager
	embedder  EmbeddingGeneratorFunc
}

// EmbeddingGeneratorFunc generates embeddings for text (used for dedup check).
type EmbeddingGeneratorFunc func(ctx context.Context, text string) ([]float32, error)

// NewExtractor creates a memory extractor.
func NewExtractor(llmClient llm.Client, model string, manager *Manager, embedder EmbeddingGeneratorFunc) *Extractor {
	return &Extractor{
		llmClient: llmClient,
		model:     model,
		manager:   manager,
		embedder:  embedder,
	}
}

// ExtractionInput contains the conversation turn to extract memories from.
type ExtractionInput struct {
	AgentID   uuid.UUID
	TenantID  uuid.UUID
	SessionID uuid.UUID
	UserMsg   string
	AssistantMsg string
}

// extractedMemory is the LLM's structured output for a single memory.
type extractedMemory struct {
	Type       string  `json:"type"`
	Content    string  `json:"content"`
	Importance float64 `json:"importance"`
}

const extractionPrompt = `Extract memories from this conversation turn. Return a JSON array only, no other text.

Each item: {"type":"episodic|semantic|procedural","content":"concise memory text","importance":0.0-1.0}

Classification rules:
- "semantic": facts, definitions, knowledge, data points the user shared
- "procedural": how-to steps, workflows, commands, tool usage patterns, solutions to problems
- "episodic": user preferences, personal context, requests, opinions, events mentioned

Importance scoring:
- User says "remember this", "don't forget", "important" → 0.9
- Key facts, solutions, or corrections → 0.7
- General conversation context → 0.5
- Trivial/obvious information → skip entirely

Rules:
- Skip greetings, small talk, confirmations ("ok", "thanks"), trivial exchanges
- Keep memory content concise but self-contained (understandable without conversation context)
- Return [] if nothing worth remembering
- Maximum 3 memories per turn

User: %s
Assistant: %s`

// ExtractAsync extracts memories from a conversation turn asynchronously.
func (e *Extractor) ExtractAsync(input ExtractionInput) {
	go func() {
		ctx := context.Background()
		if err := e.extract(ctx, input); err != nil {
			slog.Warn("memory extraction failed", "error", err, "agent_id", input.AgentID)
		}
	}()
}

func (e *Extractor) extract(ctx context.Context, input ExtractionInput) error {
	if e.llmClient == nil {
		return nil
	}

	// Truncate long messages to save tokens
	userMsg := truncate(input.UserMsg, 500)
	assistantMsg := truncate(input.AssistantMsg, 500)

	prompt := fmt.Sprintf(extractionPrompt, userMsg, assistantMsg)

	req := &llm.Request{
		Model: e.model,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		MaxTokens: 4096,
		Stream:    true,
	}

	chunks, err := e.llmClient.Stream(ctx, req)
	if err != nil {
		return fmt.Errorf("LLM call: %w", err)
	}

	var response strings.Builder
	for chunk := range chunks {
		switch chunk.Type {
		case "text":
			response.WriteString(chunk.Content)
		case "error":
			slog.Warn("memory extraction LLM error", "error", chunk.Content)
		}
	}

	raw := response.String()
	if raw == "" {
		slog.Debug("memory extraction: LLM returned empty response, skipping")
		return nil
	}

	memories, err := parseExtractedMemories(raw)
	if err != nil {
		return fmt.Errorf("parsing response: %w (raw: %s)", err, response.String())
	}

	if len(memories) == 0 {
		return nil
	}

	// Store each extracted memory
	stored := 0
	for _, m := range memories {
		if m.Content == "" || m.Type == "" {
			continue
		}
		// Validate type
		switch m.Type {
		case "episodic", "semantic", "procedural":
		default:
			m.Type = "semantic"
		}
		// Clamp importance
		if m.Importance <= 0 {
			m.Importance = 0.5
		}
		if m.Importance > 1.0 {
			m.Importance = 1.0
		}

		entry := &MemoryEntry{
			AgentID:         input.AgentID,
			TenantID:        input.TenantID,
			MemoryType:      m.Type,
			Content:         m.Content,
			ImportanceScore: m.Importance,
			SourceSessionID: &input.SessionID,
		}

		if err := e.manager.Store(ctx, entry); err != nil {
			slog.Warn("failed to store extracted memory", "error", err, "type", m.Type)
			continue
		}

		// Generate and store embedding for semantic memories
		if m.Type == "semantic" && e.embedder != nil {
			go func(id uuid.UUID, content string) {
				embedding, err := e.embedder(context.Background(), content)
				if err != nil {
					slog.Warn("failed to generate embedding", "error", err)
					return
				}
				vectorStr := vectorToString(embedding)
				_, err = e.manager.pool.Exec(context.Background(),
					`UPDATE memories SET embedding = $1::vector WHERE id = $2`,
					vectorStr, id,
				)
				if err != nil {
					slog.Warn("failed to store embedding", "error", err)
				}
			}(entry.ID, m.Content)
		}

		stored++
	}

	if stored > 0 {
		slog.Info("memories extracted",
			"count", stored,
			"agent_id", input.AgentID,
			"session_id", input.SessionID,
		)
	}

	return nil
}

func parseExtractedMemories(raw string) ([]extractedMemory, error) {
	// Strip markdown code fences if present
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		// Remove first and last line (fences)
		if len(lines) >= 3 {
			lines = lines[1 : len(lines)-1]
		}
		raw = strings.Join(lines, "\n")
		raw = strings.TrimSpace(raw)
	}

	var memories []extractedMemory
	if err := json.Unmarshal([]byte(raw), &memories); err != nil {
		return nil, err
	}
	return memories, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func vectorToString(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

package instinct

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"CapyClaw/internal/burrow/llm"
)

// Compactor performs asynchronous context compaction in a background goroutine.
type Compactor struct {
	summaryModel string
	llmClient    llm.Client
}

// NewCompactor creates a new compactor with the specified summary model and LLM client.
func NewCompactor(summaryModel string, llmClient llm.Client) *Compactor {
	return &Compactor{
		summaryModel: summaryModel,
		llmClient:    llmClient,
	}
}

// CompactAsync runs compaction in a background goroutine.
// It summarizes older messages while the agent continues operating on the current context.
func (c *Compactor) CompactAsync(ctx context.Context, sessionID string, messages []string) <-chan *CompactionResult {
	ch := make(chan *CompactionResult, 1)

	go func() {
		defer close(ch)

		slog.Info("starting async compaction",
			"session_id", sessionID,
			"message_count", len(messages),
			"model", c.summaryModel,
		)

		if c.llmClient == nil {
			ch <- &CompactionResult{
				Error: fmt.Errorf("no LLM client configured for compaction"),
			}
			return
		}

		// Build the summarization prompt
		summaryPrompt := "Compress the following conversation history into a concise summary. " +
			"Preserve all key facts, decisions, entities, numbers, code references, and context. " +
			"Use bullet points for clarity. Do not lose any critical information.\n\n" +
			strings.Join(messages, "\n")

		// Estimate tokens saved (rough: 4 chars per token)
		originalTokens := len(strings.Join(messages, "\n")) / 4

		llmReq := &llm.Request{
			Model: c.summaryModel,
			Messages: []llm.Message{
				{Role: "user", Content: summaryPrompt},
			},
			MaxTokens: 2000,
			Stream:    true,
		}

		// Call LLM for summarization
		chunks, err := c.llmClient.Stream(ctx, llmReq)
		if err != nil {
			ch <- &CompactionResult{Error: fmt.Errorf("compaction LLM call: %w", err)}
			return
		}

		var summary strings.Builder
		for chunk := range chunks {
			if chunk.Type == "text" {
				summary.WriteString(chunk.Content)
			}
			if chunk.Type == "error" {
				ch <- &CompactionResult{Error: fmt.Errorf("compaction LLM error: %s", chunk.Content)}
				return
			}
		}

		summaryTokens := summary.Len() / 4
		tokensSaved := originalTokens - summaryTokens
		if tokensSaved < 0 {
			tokensSaved = 0
		}

		slog.Info("compaction completed",
			"session_id", sessionID,
			"original_tokens", originalTokens,
			"summary_tokens", summaryTokens,
			"tokens_saved", tokensSaved,
		)

		ch <- &CompactionResult{
			Summary:           summary.String(),
			TokensSaved:       tokensSaved,
			MessagesCompacted: len(messages),
		}
	}()

	return ch
}

// CompactionResult contains the outcome of a compaction operation.
type CompactionResult struct {
	Summary           string
	TokensSaved       int
	MessagesCompacted int
	Error             error
}

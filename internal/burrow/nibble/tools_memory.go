package nibble

import (
	"context"
	"encoding/json"
	"fmt"
)

// MemorySearchFunc is the function signature for searching agent memories.
// Set at startup to avoid circular dependency with the memory package.
type MemorySearchFunc func(ctx context.Context, agentID, query string, limit int) ([]map[string]any, error)

var memorySearchFn MemorySearchFunc

// SetMemorySearchFunc sets the global memory search function.
func SetMemorySearchFunc(fn MemorySearchFunc) {
	memorySearchFn = fn
}

// registerMemoryTools registers memory search and retrieval tools.
// group:memory — memory_search
func registerMemoryTools(r *Registry) {
	r.Register(&ToolDef{
		Name:        "memory_search",
		Description: "Search the agent's memory for relevant information. Returns episodic (past conversations), semantic (facts and knowledge), and procedural (how-to skills) memories ranked by relevance.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query — describe what you want to remember or find",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return (default: 10)",
				},
			},
			"required": []string{"query"},
		},
		Source: "bundled",
	})
	RegisterBuiltin("memory_search", memorySearchHandler)
}

func memorySearchHandler(ctx context.Context, input map[string]any) (string, error) {
	if memorySearchFn == nil {
		return "Memory system not available", nil
	}

	query, _ := input["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	limit := 10
	if v, ok := input["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	// agentID is injected by the pipeline before tool execution
	agentID, _ := input["_agent_id"].(string)
	if agentID == "" {
		return "", fmt.Errorf("agent context not available")
	}

	results, err := memorySearchFn(ctx, agentID, query, limit)
	if err != nil {
		return "", fmt.Errorf("memory search: %w", err)
	}

	if len(results) == 0 {
		return "No relevant memories found", nil
	}

	data, _ := json.MarshalIndent(results, "", "  ")
	return string(data), nil
}

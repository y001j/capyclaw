package instinct

// ContextManager tracks context window usage and triggers compaction.
type ContextManager struct {
	maxTokens        int
	triggerThreshold float64
}

// NewContextManager creates a new context window manager.
func NewContextManager(maxTokens int, triggerThreshold float64) *ContextManager {
	return &ContextManager{
		maxTokens:        maxTokens,
		triggerThreshold: triggerThreshold,
	}
}

// ShouldCompact returns true if the current token count exceeds the compaction threshold.
func (m *ContextManager) ShouldCompact(currentTokens int) bool {
	threshold := float64(m.maxTokens) * m.triggerThreshold
	return float64(currentTokens) > threshold
}

// RemainingTokens returns the number of tokens available in the context window.
func (m *ContextManager) RemainingTokens(currentTokens int) int {
	remaining := m.maxTokens - currentTokens
	if remaining < 0 {
		return 0
	}
	return remaining
}

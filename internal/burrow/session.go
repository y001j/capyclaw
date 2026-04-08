package burrow

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Session represents an active agent conversation session.
type Session struct {
	ID              uuid.UUID `json:"id"`
	AgentID         uuid.UUID `json:"agent_id"`
	TenantID        uuid.UUID `json:"tenant_id"`
	SessionKey      string    `json:"session_key"`
	CompactionCount int       `json:"compaction_count"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalCostUSD    float64   `json:"total_cost_usd"`
	ContextSummary  string    `json:"context_summary"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// SessionManager manages agent sessions with goroutine-per-session model.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session // keyed by session key
}

// NewSessionManager creates a new session manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// GetOrCreate retrieves an existing session or creates a new one.
func (m *SessionManager) GetOrCreate(agentID, tenantID uuid.UUID, key string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if sess, ok := m.sessions[key]; ok {
		return sess
	}

	sess := &Session{
		ID:         uuid.New(),
		AgentID:    agentID,
		TenantID:   tenantID,
		SessionKey: key,
		Status:     "active",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.sessions[key] = sess
	return sess
}

// Get retrieves a session by ID.
func (m *SessionManager) Get(id uuid.UUID) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, sess := range m.sessions {
		if sess.ID == id {
			return sess, true
		}
	}
	return nil, false
}

// List returns all sessions.
func (m *SessionManager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		result = append(result, sess)
	}
	return result
}

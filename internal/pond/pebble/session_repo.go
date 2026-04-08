package pebble

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRepository handles session persistence in PostgreSQL.
type SessionRepository struct {
	pool *pgxpool.Pool
}

// Session represents a persisted session.
type Session struct {
	ID                uuid.UUID `json:"id"`
	TenantID          uuid.UUID `json:"tenant_id"`
	AgentID           uuid.UUID `json:"agent_id"`
	SessionKey        string    `json:"session_key"`
	CompactionCount   int       `json:"compaction_count"`
	TotalInputTokens  int64     `json:"total_input_tokens"`
	TotalOutputTokens int64     `json:"total_output_tokens"`
	TotalCostUSD      float64   `json:"total_cost_usd"`
	ContextSummary    *string   `json:"context_summary"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// NewSessionRepository creates a new session repository.
func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

// GetOrCreate retrieves an existing session or creates a new one.
func (r *SessionRepository) GetOrCreate(ctx context.Context, agentID, tenantID uuid.UUID, key string) (*Session, error) {
	sess := &Session{}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO sessions (agent_id, tenant_id, session_key)
		VALUES ($1, $2, $3)
		ON CONFLICT (agent_id, session_key) DO UPDATE SET updated_at = NOW()
		RETURNING id, tenant_id, agent_id, session_key, compaction_count,
				  total_input_tokens, total_output_tokens, total_cost_usd,
				  context_summary, status, created_at, updated_at
	`, agentID, tenantID, key).Scan(
		&sess.ID, &sess.TenantID, &sess.AgentID, &sess.SessionKey,
		&sess.CompactionCount, &sess.TotalInputTokens, &sess.TotalOutputTokens,
		&sess.TotalCostUSD, &sess.ContextSummary, &sess.Status,
		&sess.CreatedAt, &sess.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get or create session: %w", err)
	}
	return sess, nil
}

// Get retrieves a session by ID.
func (r *SessionRepository) Get(ctx context.Context, id uuid.UUID) (*Session, error) {
	sess := &Session{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, agent_id, session_key, compaction_count,
			   total_input_tokens, total_output_tokens, total_cost_usd,
			   context_summary, status, created_at, updated_at
		FROM sessions WHERE id = $1
	`, id).Scan(
		&sess.ID, &sess.TenantID, &sess.AgentID, &sess.SessionKey,
		&sess.CompactionCount, &sess.TotalInputTokens, &sess.TotalOutputTokens,
		&sess.TotalCostUSD, &sess.ContextSummary, &sess.Status,
		&sess.CreatedAt, &sess.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return sess, nil
}

// ListByAgent returns all sessions for an agent.
func (r *SessionRepository) ListByAgent(ctx context.Context, agentID uuid.UUID, limit, offset int) ([]*Session, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, agent_id, session_key, compaction_count,
			   total_input_tokens, total_output_tokens, total_cost_usd,
			   context_summary, status, created_at, updated_at
		FROM sessions WHERE agent_id = $1
		ORDER BY updated_at DESC
		LIMIT $2 OFFSET $3
	`, agentID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		s := &Session{}
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.AgentID, &s.SessionKey,
			&s.CompactionCount, &s.TotalInputTokens, &s.TotalOutputTokens,
			&s.TotalCostUSD, &s.ContextSummary, &s.Status,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// Archive sets a session's status to 'archived'.
func (r *SessionRepository) Archive(ctx context.Context, id, tenantID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE sessions SET status = 'archived', updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
	`, id, tenantID)
	if err != nil {
		return fmt.Errorf("archive session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("archive session: not found")
	}
	return nil
}

// UpdateCompaction updates the compaction summary and count for a session.
func (r *SessionRepository) UpdateCompaction(ctx context.Context, id uuid.UUID, summary string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE sessions
		SET context_summary = $2, compaction_count = compaction_count + 1, updated_at = NOW()
		WHERE id = $1
	`, id, summary)
	if err != nil {
		return fmt.Errorf("update compaction: %w", err)
	}
	return nil
}

// UpdateTokenCounts increments the token and cost counters for a session.
func (r *SessionRepository) UpdateTokenCounts(ctx context.Context, sessionID uuid.UUID, inputTokens, outputTokens int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE sessions
		SET total_input_tokens = total_input_tokens + $2,
			total_output_tokens = total_output_tokens + $3,
			updated_at = NOW()
		WHERE id = $1
	`, sessionID, inputTokens, outputTokens)
	if err != nil {
		return fmt.Errorf("update session token counts: %w", err)
	}
	return nil
}

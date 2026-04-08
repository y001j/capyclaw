package pebble

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MessageRepository handles message persistence in PostgreSQL.
type MessageRepository struct {
	pool *pgxpool.Pool
}

// Message represents a persisted conversation message.
type Message struct {
	ID        uuid.UUID       `json:"id"`
	SessionID uuid.UUID       `json:"session_id"`
	TenantID  uuid.UUID       `json:"tenant_id"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Model     *string         `json:"model,omitempty"`
	Usage     json.RawMessage `json:"usage,omitempty"`
	ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
	ParentID  *uuid.UUID      `json:"parent_id,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// NewMessageRepository creates a new message repository.
func NewMessageRepository(pool *pgxpool.Pool) *MessageRepository {
	return &MessageRepository{pool: pool}
}

// Append inserts a new message.
func (r *MessageRepository) Append(ctx context.Context, msg *Message) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO messages (session_id, tenant_id, role, content, model, usage, tool_calls, parent_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, msg.SessionID, msg.TenantID, msg.Role, msg.Content, msg.Model,
		msg.Usage, msg.ToolCalls, msg.ParentID)
	if err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	return nil
}

// ListBySession returns messages for a session with pagination.
func (r *MessageRepository) ListBySession(ctx context.Context, sessionID uuid.UUID, limit, offset int) ([]*Message, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, session_id, tenant_id, role, content, model, usage, tool_calls, parent_id, created_at
		FROM messages WHERE session_id = $1
		ORDER BY created_at ASC
		LIMIT $2 OFFSET $3
	`, sessionID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		m := &Message{}
		if err := rows.Scan(
			&m.ID, &m.SessionID, &m.TenantID, &m.Role, &m.Content,
			&m.Model, &m.Usage, &m.ToolCalls, &m.ParentID, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, nil
}

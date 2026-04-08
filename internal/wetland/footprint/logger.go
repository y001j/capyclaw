package footprint

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Logger writes immutable audit events to PostgreSQL.
// The audit_log table is INSERT-only (no UPDATE or DELETE grants).
type Logger struct {
	pool *pgxpool.Pool
}

// NewLogger creates a new audit logger.
func NewLogger(pool *pgxpool.Pool) *Logger {
	return &Logger{pool: pool}
}

// Log writes an audit event.
func (l *Logger) Log(ctx context.Context, event *AuditEvent) error {
	_, err := l.pool.Exec(ctx, `
		INSERT INTO audit_log (tenant_id, actor_id, actor_type, action, resource_type, resource_id, details, ip_address, user_agent, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, event.TenantID, event.ActorID, event.ActorType, event.Action,
		event.ResourceType, event.ResourceID, event.Details,
		event.IPAddress, event.UserAgent, event.RequestID)
	if err != nil {
		return fmt.Errorf("logging audit event: %w", err)
	}
	return nil
}

// Query retrieves audit events matching the filter.
func (l *Logger) Query(ctx context.Context, filter AuditFilter) ([]*AuditEvent, error) {
	query := `
		SELECT id, tenant_id, actor_id, actor_type, action,
		       resource_type, resource_id, details,
		       ip_address, user_agent, request_id, created_at
		FROM audit_log
		WHERE 1=1
	`
	args := []any{}
	argIdx := 1

	if filter.TenantID != nil {
		query += fmt.Sprintf(" AND tenant_id = $%d", argIdx)
		args = append(args, *filter.TenantID)
		argIdx++
	}
	if filter.ActorID != nil {
		query += fmt.Sprintf(" AND actor_id = $%d", argIdx)
		args = append(args, *filter.ActorID)
		argIdx++
	}
	if filter.Action != "" {
		query += fmt.Sprintf(" AND action = $%d", argIdx)
		args = append(args, filter.Action)
		argIdx++
	}
	if filter.ResourceType != "" {
		query += fmt.Sprintf(" AND resource_type = $%d", argIdx)
		args = append(args, filter.ResourceType)
		argIdx++
	}
	if filter.Since != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *filter.Since)
		argIdx++
	}
	if filter.Until != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, *filter.Until)
		argIdx++
	}

	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := l.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying audit log: %w", err)
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		e := &AuditEvent{}
		if err := rows.Scan(
			&e.ID, &e.TenantID, &e.ActorID, &e.ActorType, &e.Action,
			&e.ResourceType, &e.ResourceID, &e.Details,
			&e.IPAddress, &e.UserAgent, &e.RequestID, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning audit event: %w", err)
		}
		events = append(events, e)
	}
	return events, nil
}

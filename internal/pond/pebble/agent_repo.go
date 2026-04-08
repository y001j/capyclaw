package pebble

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentRepository handles agent configuration persistence.
type AgentRepository struct {
	pool *pgxpool.Pool
}

// Agent represents a persisted agent configuration.
type Agent struct {
	ID             uuid.UUID       `json:"id"`
	TenantID       uuid.UUID       `json:"tenant_id"`
	Name           string          `json:"name"`
	Slug           string          `json:"slug"`
	Model          string          `json:"model"`
	FallbackModels []string        `json:"fallback_models,omitempty"`
	SystemPrompt   *string         `json:"system_prompt,omitempty"`
	IdentityMD     *string         `json:"identity_md,omitempty"`
	SoulMD         *string         `json:"soul_md,omitempty"`
	UserMD         *string         `json:"user_md,omitempty"`
	Settings       json.RawMessage `json:"settings,omitempty"`
	ToolPolicy     json.RawMessage `json:"tool_policy,omitempty"`
	Status         string          `json:"status"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// NewAgentRepository creates a new agent repository.
func NewAgentRepository(pool *pgxpool.Pool) *AgentRepository {
	return &AgentRepository{pool: pool}
}

// Create inserts a new agent and returns the full record.
func (r *AgentRepository) Create(ctx context.Context, a *Agent) error {
	if a.Settings == nil {
		a.Settings = json.RawMessage(`{}`)
	}
	if a.ToolPolicy == nil {
		a.ToolPolicy = json.RawMessage(`{}`)
	}
	if a.FallbackModels == nil {
		a.FallbackModels = []string{}
	}

	err := r.pool.QueryRow(ctx, `
		INSERT INTO agents (tenant_id, name, slug, model, fallback_models, system_prompt,
			identity_md, soul_md, user_md, settings, tool_policy)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, status, created_at, updated_at
	`, a.TenantID, a.Name, a.Slug, a.Model, a.FallbackModels, a.SystemPrompt,
		a.IdentityMD, a.SoulMD, a.UserMD, a.Settings, a.ToolPolicy,
	).Scan(&a.ID, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	return nil
}

// Get retrieves an agent by ID.
func (r *AgentRepository) Get(ctx context.Context, id uuid.UUID) (*Agent, error) {
	a := &Agent{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, slug, model, fallback_models, system_prompt,
			identity_md, soul_md, user_md, settings, tool_policy, status, created_at, updated_at
		FROM agents WHERE id = $1
	`, id).Scan(&a.ID, &a.TenantID, &a.Name, &a.Slug, &a.Model, &a.FallbackModels,
		&a.SystemPrompt, &a.IdentityMD, &a.SoulMD, &a.UserMD,
		&a.Settings, &a.ToolPolicy, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	return a, nil
}

// ListByTenant returns all active agents for a tenant with pagination.
func (r *AgentRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*Agent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, slug, model, fallback_models, system_prompt,
			identity_md, soul_md, user_md, settings, tool_policy, status, created_at, updated_at
		FROM agents WHERE tenant_id = $1 AND status != 'archived'
		ORDER BY name ASC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []*Agent
	for rows.Next() {
		a := &Agent{}
		if err := rows.Scan(&a.ID, &a.TenantID, &a.Name, &a.Slug, &a.Model, &a.FallbackModels,
			&a.SystemPrompt, &a.IdentityMD, &a.SoulMD, &a.UserMD,
			&a.Settings, &a.ToolPolicy, &a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning agent: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// AgentUpdate holds the fields that can be updated (PATCH semantics).
type AgentUpdate struct {
	Name           *string          `json:"name,omitempty"`
	Model          *string          `json:"model,omitempty"`
	FallbackModels *[]string        `json:"fallback_models,omitempty"`
	SystemPrompt   *string          `json:"system_prompt,omitempty"`
	IdentityMD     *string          `json:"identity_md,omitempty"`
	SoulMD         *string          `json:"soul_md,omitempty"`
	UserMD         *string          `json:"user_md,omitempty"`
	Settings       *json.RawMessage `json:"settings,omitempty"`
	ToolPolicy     *json.RawMessage `json:"tool_policy,omitempty"`
}

// Update performs a partial update on an agent (PATCH semantics).
func (r *AgentRepository) Update(ctx context.Context, id uuid.UUID, upd *AgentUpdate) (*Agent, error) {
	setClauses := []string{}
	args := []any{}
	argIdx := 1

	if upd.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *upd.Name)
		argIdx++
	}
	if upd.Model != nil {
		setClauses = append(setClauses, fmt.Sprintf("model = $%d", argIdx))
		args = append(args, *upd.Model)
		argIdx++
	}
	if upd.FallbackModels != nil {
		setClauses = append(setClauses, fmt.Sprintf("fallback_models = $%d", argIdx))
		args = append(args, *upd.FallbackModels)
		argIdx++
	}
	if upd.SystemPrompt != nil {
		setClauses = append(setClauses, fmt.Sprintf("system_prompt = $%d", argIdx))
		args = append(args, *upd.SystemPrompt)
		argIdx++
	}
	if upd.IdentityMD != nil {
		setClauses = append(setClauses, fmt.Sprintf("identity_md = $%d", argIdx))
		args = append(args, *upd.IdentityMD)
		argIdx++
	}
	if upd.SoulMD != nil {
		setClauses = append(setClauses, fmt.Sprintf("soul_md = $%d", argIdx))
		args = append(args, *upd.SoulMD)
		argIdx++
	}
	if upd.UserMD != nil {
		setClauses = append(setClauses, fmt.Sprintf("user_md = $%d", argIdx))
		args = append(args, *upd.UserMD)
		argIdx++
	}
	if upd.Settings != nil {
		setClauses = append(setClauses, fmt.Sprintf("settings = $%d", argIdx))
		args = append(args, *upd.Settings)
		argIdx++
	}
	if upd.ToolPolicy != nil {
		setClauses = append(setClauses, fmt.Sprintf("tool_policy = $%d", argIdx))
		args = append(args, *upd.ToolPolicy)
		argIdx++
	}

	if len(setClauses) == 0 {
		return r.Get(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE agents SET %s
		WHERE id = $%d
		RETURNING id, tenant_id, name, slug, model, fallback_models, system_prompt,
			identity_md, soul_md, user_md, settings, tool_policy, status, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIdx)

	a := &Agent{}
	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&a.ID, &a.TenantID, &a.Name, &a.Slug, &a.Model, &a.FallbackModels,
		&a.SystemPrompt, &a.IdentityMD, &a.SoulMD, &a.UserMD,
		&a.Settings, &a.ToolPolicy, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update agent: %w", err)
	}
	return a, nil
}

// SoftDelete archives an agent (sets status to 'archived').
// Appends a timestamp suffix to the slug to free it for reuse.
func (r *AgentRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	suffix := fmt.Sprintf("_deleted_%d", time.Now().UnixMilli())
	ct, err := r.pool.Exec(ctx,
		"UPDATE agents SET status = 'archived', slug = slug || $1, updated_at = NOW() WHERE id = $2",
		suffix, id)
	if err != nil {
		return fmt.Errorf("soft delete agent: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("agent not found")
	}
	return nil
}

// Delete removes an agent by ID (hard delete).
func (r *AgentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM agents WHERE id = $1", id)
	return err
}

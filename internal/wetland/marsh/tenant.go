package marsh

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tenant represents a CapyClaw tenant.
type Tenant struct {
	ID              uuid.UUID        `json:"id"`
	Name            string           `json:"name"`
	Slug            string           `json:"slug"`
	Plan            string           `json:"plan"`
	Status          string           `json:"status"`
	Quota           json.RawMessage  `json:"quota,omitempty"`
	SuspendedAt     *time.Time       `json:"suspended_at,omitempty"`
	SuspendedReason *string          `json:"suspended_reason,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// TenantManager handles multi-tenant isolation and management.
type TenantManager struct {
	pool *pgxpool.Pool
}

// NewTenantManager creates a new tenant manager.
func NewTenantManager(pool *pgxpool.Pool) *TenantManager {
	return &TenantManager{pool: pool}
}

// Get retrieves a tenant by ID.
func (m *TenantManager) Get(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	t := &Tenant{}
	err := m.pool.QueryRow(ctx, `
		SELECT id, name, slug, plan, status, quota, suspended_at, suspended_reason, created_at, updated_at
		FROM tenants WHERE id = $1
	`, id).Scan(&t.ID, &t.Name, &t.Slug, &t.Plan, &t.Status, &t.Quota, &t.SuspendedAt, &t.SuspendedReason, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return t, nil
}

// Create inserts a new tenant.
func (m *TenantManager) Create(ctx context.Context, name, slug, plan string) (*Tenant, error) {
	t := &Tenant{}
	err := m.pool.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan)
		VALUES ($1, $2, $3)
		RETURNING id, name, slug, plan, status, quota, suspended_at, suspended_reason, created_at, updated_at
	`, name, slug, plan).Scan(&t.ID, &t.Name, &t.Slug, &t.Plan, &t.Status, &t.Quota, &t.SuspendedAt, &t.SuspendedReason, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}
	return t, nil
}

// List returns all tenants.
func (m *TenantManager) List(ctx context.Context) ([]*Tenant, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT id, name, slug, plan, status, quota, suspended_at, suspended_reason, created_at, updated_at
		FROM tenants ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*Tenant
	for rows.Next() {
		t := &Tenant{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Plan, &t.Status, &t.Quota, &t.SuspendedAt, &t.SuspendedReason, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tenants = append(tenants, t)
	}
	return tenants, nil
}

// TenantUpdate represents fields that can be updated on a tenant.
type TenantUpdate struct {
	Name   *string          `json:"name,omitempty"`
	Plan   *string          `json:"plan,omitempty"`
	Status *string          `json:"status,omitempty"`
	Quota  *json.RawMessage `json:"quota,omitempty"`
}

// Update applies partial updates to a tenant.
func (m *TenantManager) Update(ctx context.Context, id uuid.UUID, updates TenantUpdate) (*Tenant, error) {
	query := "UPDATE tenants SET updated_at = NOW()"
	args := []any{}
	argIdx := 1

	if updates.Name != nil {
		query += fmt.Sprintf(", name = $%d", argIdx)
		args = append(args, *updates.Name)
		argIdx++
	}
	if updates.Plan != nil {
		query += fmt.Sprintf(", plan = $%d", argIdx)
		args = append(args, *updates.Plan)
		argIdx++
	}
	if updates.Status != nil {
		query += fmt.Sprintf(", status = $%d", argIdx)
		args = append(args, *updates.Status)
		argIdx++
	}
	if updates.Quota != nil {
		query += fmt.Sprintf(", quota = $%d", argIdx)
		args = append(args, *updates.Quota)
		argIdx++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argIdx)
	args = append(args, id)
	argIdx++

	query += " RETURNING id, name, slug, plan, status, quota, suspended_at, suspended_reason, created_at, updated_at"

	t := &Tenant{}
	err := m.pool.QueryRow(ctx, query, args...).Scan(
		&t.ID, &t.Name, &t.Slug, &t.Plan, &t.Status, &t.Quota,
		&t.SuspendedAt, &t.SuspendedReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update tenant: %w", err)
	}
	return t, nil
}

// Suspend suspends a tenant with a reason.
func (m *TenantManager) Suspend(ctx context.Context, id uuid.UUID, reason string) (*Tenant, error) {
	t := &Tenant{}
	err := m.pool.QueryRow(ctx, `
		UPDATE tenants SET status = 'suspended', suspended_at = NOW(), suspended_reason = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, slug, plan, status, quota, suspended_at, suspended_reason, created_at, updated_at
	`, id, reason).Scan(
		&t.ID, &t.Name, &t.Slug, &t.Plan, &t.Status, &t.Quota,
		&t.SuspendedAt, &t.SuspendedReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("suspend tenant: %w", err)
	}
	return t, nil
}

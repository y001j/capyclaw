package marsh

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QuotaManager enforces per-tenant resource limits.
type QuotaManager struct {
	pool *pgxpool.Pool
}

// NewQuotaManager creates a new quota manager.
func NewQuotaManager(pool *pgxpool.Pool) *QuotaManager {
	return &QuotaManager{pool: pool}
}

// UsageRequest describes the resources being requested.
type UsageRequest struct {
	TokensUsed int64
	CostUSD    float64
}

// TenantQuota represents the quota configuration from the tenants.quota JSONB field.
type TenantQuota struct {
	MaxAgents             int     `json:"max_agents"`
	MaxSessionsPerAgent   int     `json:"max_sessions_per_agent"`
	MonthlyTokenBudget    int64   `json:"monthly_token_budget"`
	MonthlyCostLimitUSD   float64 `json:"monthly_cost_limit_usd"`
	AlertThreshold        float64 `json:"alert_threshold"`
}

// CheckQuota verifies whether a tenant has remaining budget.
func (m *QuotaManager) CheckQuota(ctx context.Context, tenantID uuid.UUID, usage UsageRequest) (bool, error) {
	// 1. Fetch tenant quota configuration
	var quotaJSON json.RawMessage
	err := m.pool.QueryRow(ctx, `SELECT quota FROM tenants WHERE id = $1`, tenantID).Scan(&quotaJSON)
	if err != nil {
		return false, fmt.Errorf("fetching tenant quota: %w", err)
	}

	var quota TenantQuota
	if err := json.Unmarshal(quotaJSON, &quota); err != nil {
		return false, fmt.Errorf("parsing tenant quota: %w", err)
	}

	// 2. Aggregate current month's usage from sessions
	var currentTokens int64
	var currentCost float64
	err = m.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(total_input_tokens + total_output_tokens), 0),
		       COALESCE(SUM(total_cost_usd), 0)
		FROM sessions
		WHERE tenant_id = $1
		  AND created_at >= date_trunc('month', NOW())
	`, tenantID).Scan(&currentTokens, &currentCost)
	if err != nil {
		return false, fmt.Errorf("aggregating monthly usage: %w", err)
	}

	// 3. Check if new usage would exceed budget
	if quota.MonthlyTokenBudget > 0 && currentTokens+usage.TokensUsed > quota.MonthlyTokenBudget {
		return false, nil
	}
	if quota.MonthlyCostLimitUSD > 0 && currentCost+usage.CostUSD > quota.MonthlyCostLimitUSD {
		return false, nil
	}

	return true, nil
}

// RecordUsage records token and cost usage for a tenant into the monthly aggregation table.
func (m *QuotaManager) RecordUsage(ctx context.Context, tenantID uuid.UUID, model string, inputTokens, outputTokens int64, costUSD float64) error {
	_, err := m.pool.Exec(ctx, `
		INSERT INTO usage_monthly (tenant_id, month, model, input_tokens, output_tokens, cost_usd, session_count, updated_at)
		VALUES ($1, date_trunc('month', NOW()), $2, $3, $4, $5, 1, NOW())
		ON CONFLICT (tenant_id, month, model) DO UPDATE SET
			input_tokens = usage_monthly.input_tokens + EXCLUDED.input_tokens,
			output_tokens = usage_monthly.output_tokens + EXCLUDED.output_tokens,
			cost_usd = usage_monthly.cost_usd + EXCLUDED.cost_usd,
			session_count = usage_monthly.session_count + EXCLUDED.session_count,
			updated_at = NOW()
	`, tenantID, model, inputTokens, outputTokens, costUSD)
	if err != nil {
		return fmt.Errorf("recording usage: %w", err)
	}
	return nil
}

// CheckAlert checks whether a tenant is approaching their budget limit.
// Returns whether an alert should be sent and the current usage percentage.
func (m *QuotaManager) CheckAlert(ctx context.Context, tenantID uuid.UUID) (bool, float64, error) {
	var quotaJSON json.RawMessage
	err := m.pool.QueryRow(ctx, `SELECT quota FROM tenants WHERE id = $1`, tenantID).Scan(&quotaJSON)
	if err != nil {
		return false, 0, fmt.Errorf("fetching tenant quota: %w", err)
	}

	var quota TenantQuota
	if err := json.Unmarshal(quotaJSON, &quota); err != nil {
		return false, 0, fmt.Errorf("parsing tenant quota: %w", err)
	}

	if quota.MonthlyTokenBudget <= 0 {
		return false, 0, nil
	}

	var currentTokens int64
	err = m.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(total_input_tokens + total_output_tokens), 0)
		FROM sessions
		WHERE tenant_id = $1
		  AND created_at >= date_trunc('month', NOW())
	`, tenantID).Scan(&currentTokens)
	if err != nil {
		return false, 0, fmt.Errorf("aggregating monthly usage: %w", err)
	}

	percentage := float64(currentTokens) / float64(quota.MonthlyTokenBudget)
	threshold := quota.AlertThreshold
	if threshold <= 0 {
		threshold = 0.8
	}

	return percentage >= threshold, percentage, nil
}

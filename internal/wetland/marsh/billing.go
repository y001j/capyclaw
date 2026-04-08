package marsh

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"CapyClaw/internal/wetland/footprint"
)

// BillingTracker tracks LLM costs per tenant.
type BillingTracker struct {
	pool         *pgxpool.Pool
	quotaManager *QuotaManager
	auditLogger  *footprint.Logger
}

// NewBillingTracker creates a new billing tracker.
func NewBillingTracker(pool *pgxpool.Pool, qm *QuotaManager, logger *footprint.Logger) *BillingTracker {
	return &BillingTracker{
		pool:         pool,
		quotaManager: qm,
		auditLogger:  logger,
	}
}

// RecordLLMUsage records LLM API usage and checks budget alerts.
func (b *BillingTracker) RecordLLMUsage(ctx context.Context, tenantID uuid.UUID, provider, model string, inputTokens, outputTokens int, costUSD float64) error {
	// Record to monthly aggregation
	if err := b.quotaManager.RecordUsage(ctx, tenantID, model, int64(inputTokens), int64(outputTokens), costUSD); err != nil {
		slog.Warn("failed to record usage", "error", err)
	}

	// Log audit event with cost details
	if b.auditLogger != nil {
		details, _ := json.Marshal(map[string]any{
			"provider":      provider,
			"model":         model,
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"cost_usd":      costUSD,
		})
		_ = b.auditLogger.Log(ctx, &footprint.AuditEvent{
			TenantID:  tenantID,
			ActorType: "system",
			Action:    footprint.ActionLLMRequest,
			Details:   details,
		})
	}

	// Check budget alert threshold
	alert, percentage, err := b.quotaManager.CheckAlert(ctx, tenantID)
	if err != nil {
		slog.Warn("failed to check budget alert", "error", err)
		return nil
	}
	if alert {
		slog.Warn("tenant approaching budget limit",
			"tenant_id", tenantID,
			"usage_percentage", fmt.Sprintf("%.1f%%", percentage*100),
		)
	}

	return nil
}

// UsageReport represents a usage report for a tenant.
type UsageReport struct {
	TenantID  uuid.UUID    `json:"tenant_id"`
	StartDate time.Time    `json:"start_date"`
	EndDate   time.Time    `json:"end_date"`
	Models    []ModelUsage `json:"models"`
	Totals    UsageTotals  `json:"totals"`
}

// ModelUsage represents usage breakdown by model.
type ModelUsage struct {
	Model        string  `json:"model"`
	SessionCount int     `json:"session_count"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// UsageTotals represents total usage across all models.
type UsageTotals struct {
	SessionCount int     `json:"session_count"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// GenerateUsageReport generates a usage report for a tenant over a date range.
func (b *BillingTracker) GenerateUsageReport(ctx context.Context, tenantID uuid.UUID, startDate, endDate time.Time) (*UsageReport, error) {
	rows, err := b.pool.Query(ctx, `
		SELECT COALESCE(m.model, 'unknown'),
		       COUNT(DISTINCT s.id) as session_count,
		       COALESCE(SUM(s.total_input_tokens), 0) as input_tokens,
		       COALESCE(SUM(s.total_output_tokens), 0) as output_tokens,
		       COALESCE(SUM(s.total_cost_usd), 0) as cost_usd
		FROM sessions s
		LEFT JOIN LATERAL (
			SELECT DISTINCT model FROM messages WHERE session_id = s.id AND model IS NOT NULL LIMIT 1
		) m ON true
		WHERE s.tenant_id = $1
		  AND s.created_at >= $2
		  AND s.created_at <= $3
		GROUP BY m.model
		ORDER BY cost_usd DESC
	`, tenantID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("generating usage report: %w", err)
	}
	defer rows.Close()

	report := &UsageReport{
		TenantID:  tenantID,
		StartDate: startDate,
		EndDate:   endDate,
	}

	for rows.Next() {
		var mu ModelUsage
		if err := rows.Scan(&mu.Model, &mu.SessionCount, &mu.InputTokens, &mu.OutputTokens, &mu.CostUSD); err != nil {
			return nil, fmt.Errorf("scanning usage row: %w", err)
		}
		report.Models = append(report.Models, mu)
		report.Totals.SessionCount += mu.SessionCount
		report.Totals.InputTokens += mu.InputTokens
		report.Totals.OutputTokens += mu.OutputTokens
		report.Totals.CostUSD += mu.CostUSD
	}

	return report, nil
}

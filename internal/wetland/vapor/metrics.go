package vapor

import (
	"go.opentelemetry.io/otel/metric"
)

// Metrics holds all custom CapyClaw metrics.
type Metrics struct {
	// Gateway
	WSConnectionsActive metric.Int64UpDownCounter
	WSMessagesTotal     metric.Int64Counter
	HTTPRequestsTotal   metric.Int64Counter

	// Agent
	AgentSessionsActive metric.Int64UpDownCounter
	ToolExecutionsTotal metric.Int64Counter

	// LLM
	LLMRequestsTotal metric.Int64Counter
	LLMTokensTotal   metric.Int64Counter
	LLMCostTotal     metric.Float64Counter

	// Tenant
	TenantRateLimitHits metric.Int64Counter
}

// NewMetrics registers all custom metrics with the OTel meter.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	m := &Metrics{}
	var err error

	m.WSConnectionsActive, err = meter.Int64UpDownCounter("capyclaw_ws_connections_active")
	if err != nil {
		return nil, err
	}

	m.WSMessagesTotal, err = meter.Int64Counter("capyclaw_ws_messages_total")
	if err != nil {
		return nil, err
	}

	m.HTTPRequestsTotal, err = meter.Int64Counter("capyclaw_http_requests_total")
	if err != nil {
		return nil, err
	}

	m.AgentSessionsActive, err = meter.Int64UpDownCounter("capyclaw_agent_sessions_active")
	if err != nil {
		return nil, err
	}

	m.ToolExecutionsTotal, err = meter.Int64Counter("capyclaw_agent_tool_executions_total")
	if err != nil {
		return nil, err
	}

	m.LLMRequestsTotal, err = meter.Int64Counter("capyclaw_llm_requests_total")
	if err != nil {
		return nil, err
	}

	m.LLMTokensTotal, err = meter.Int64Counter("capyclaw_llm_tokens_total")
	if err != nil {
		return nil, err
	}

	m.LLMCostTotal, err = meter.Float64Counter("capyclaw_llm_cost_usd_total")
	if err != nil {
		return nil, err
	}

	m.TenantRateLimitHits, err = meter.Int64Counter("capyclaw_tenant_rate_limit_hits")
	if err != nil {
		return nil, err
	}

	return m, nil
}

package footprint

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AuditEvent represents an immutable audit log entry.
type AuditEvent struct {
	ID           int64           `json:"id"`
	TenantID     uuid.UUID       `json:"tenant_id"`
	ActorID      *uuid.UUID      `json:"actor_id,omitempty"`
	ActorType    string          `json:"actor_type"` // user, agent, system, webhook
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type,omitempty"`
	ResourceID   *uuid.UUID      `json:"resource_id,omitempty"`
	Details      json.RawMessage `json:"details"`
	IPAddress    string          `json:"ip_address,omitempty"`
	UserAgent    string          `json:"user_agent,omitempty"`
	RequestID    string          `json:"request_id,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

// AuditFilter contains parameters for querying the audit log.
type AuditFilter struct {
	TenantID     *uuid.UUID
	ActorID      *uuid.UUID
	Action       string
	ResourceType string
	Since        *time.Time
	Until        *time.Time
	Limit        int
	Offset       int
}

// Common audit action constants.
const (
	ActionSessionCreate   = "session.create"
	ActionSessionArchive  = "session.archive"
	ActionToolExecute     = "tool.execute"
	ActionToolApprove     = "tool.approve"
	ActionToolReject      = "tool.reject"
	ActionAgentCreate     = "agent.create"
	ActionAgentUpdate     = "agent.update"
	ActionAgentDelete     = "agent.delete"
	ActionSkillInstall    = "skill.install"
	ActionSkillUninstall  = "skill.uninstall"
	ActionAuthLogin       = "auth.login"
	ActionAuthLogout      = "auth.logout"
	ActionAuthFailed      = "auth.failed"
	ActionDevicePair      = "device.pair"
	ActionDeviceRevoke    = "device.revoke"
	ActionConfigChange    = "config.change"
	ActionLLMRequest      = "llm.request"
	ActionTenantCreate    = "tenant.create"
	ActionTenantUpdate    = "tenant.update"
	ActionTenantSuspend   = "tenant.suspend"
	ActionQuotaExceeded   = "quota.exceeded"
	ActionSecretRotated   = "secret.rotated"
	ActionCronCreate      = "cron.create"
	ActionCronUpdate      = "cron.update"
	ActionCronDelete      = "cron.delete"
)

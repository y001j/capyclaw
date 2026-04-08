package burrow

import (
	"context"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"CapyClaw/internal/burrow/instinct"
)

// Agent represents a configured AI agent with its runtime state.
type Agent struct {
	id       uuid.UUID
	tenantID uuid.UUID
	slug     string
	config   AgentConfig

	sessions *SessionManager
	pipeline *Pipeline
	mu       sync.RWMutex
}

// AgentConfig holds the agent's configuration.
type AgentConfig struct {
	Name           string   `json:"name"`
	Slug           string   `json:"slug"`
	Model          string   `json:"model"`
	FallbackModels []string `json:"fallback_models"`
	SystemPrompt   string   `json:"system_prompt"`
	IdentityMD     string   `json:"identity_md"`
	SoulMD         string   `json:"soul_md"`
	UserMD         string   `json:"user_md"`
	ToolPolicy     map[string]string `json:"tool_policy"`

	// Populated by WorkspaceLoader
	Skills        []instinct.SkillPrompt `json:"-"`
	MemoryContext string                 `json:"-"`
}

// NewAgent creates a new agent instance.
func NewAgent(id, tenantID uuid.UUID, cfg AgentConfig, pipeline *Pipeline) *Agent {
	return &Agent{
		id:       id,
		tenantID: tenantID,
		slug:     cfg.Slug,
		config:   cfg,
		pipeline: pipeline,
		sessions: NewSessionManager(),
	}
}

func (a *Agent) ID() uuid.UUID       { return a.id }
func (a *Agent) TenantID() uuid.UUID  { return a.tenantID }
func (a *Agent) Slug() string         { return a.slug }

// ProcessMessage sends a message through the agent's pipeline and returns a streaming response.
func (a *Agent) ProcessMessage(ctx context.Context, msg *IncomingMessage) (<-chan *StreamChunk, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	slog.Info("processing message",
		"agent_id", a.id,
		"session_key", msg.SessionKey,
	)

	req := &PipelineRequest{
		AgentID:    a.id,
		TenantID:   a.tenantID,
		Message:    msg,
		AgentConfig: a.config,
	}

	return a.pipeline.Execute(ctx, req)
}

// Shutdown gracefully shuts down the agent.
func (a *Agent) Shutdown(ctx context.Context) error {
	slog.Info("shutting down agent", "agent_id", a.id, "slug", a.slug)
	return nil
}

// IncomingMessage represents a message to be processed by an agent.
type IncomingMessage struct {
	SessionKey string `json:"session_key"`
	Content    string `json:"content"`
	Role       string `json:"role"`
	UserID     string `json:"user_id,omitempty"`
}

// StreamChunk is a piece of streaming response from the agent.
type StreamChunk struct {
	Type    string `json:"type"` // "text", "tool_call", "tool_result", "done", "error"
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

package herd

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

// AgentFactory creates agent instances for the herd manager.
// This avoids circular imports with the burrow package.
type AgentFactory interface {
	CreateAgent(id, tenantID uuid.UUID, slug string) (AgentRunner, error)
}

// AgentRunner is the interface for executing agent tasks.
type AgentRunner interface {
	ProcessMessage(ctx context.Context, content string, sessionKey string) (<-chan StreamResult, error)
	Shutdown(ctx context.Context) error
}

// StreamResult represents a piece of streaming output from an agent.
type StreamResult struct {
	Type    string `json:"type"` // "text", "tool_call", "tool_result", "done", "error"
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Manager handles multi-agent spawning and lifecycle management.
type Manager struct {
	mu       sync.RWMutex
	agents   map[uuid.UUID]*AgentHandle
	tenantID uuid.UUID
	factory  AgentFactory
}

// AgentHandle represents a running agent in the herd.
type AgentHandle struct {
	ID       uuid.UUID
	ParentID uuid.UUID
	Slug     string
	Status   string
	Cancel   context.CancelFunc
	Runner   AgentRunner
	ResultCh chan *StreamResult
}

// NewManager creates a new herd manager.
func NewManager(tenantID uuid.UUID, factory AgentFactory) *Manager {
	return &Manager{
		agents:   make(map[uuid.UUID]*AgentHandle),
		tenantID: tenantID,
		factory:  factory,
	}
}

// Spawn creates and starts a new sub-agent.
func (m *Manager) Spawn(ctx context.Context, parentID uuid.UUID, slug string) (*AgentHandle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := uuid.New()
	ctx, cancel := context.WithCancel(ctx)

	var runner AgentRunner
	if m.factory != nil {
		var err error
		runner, err = m.factory.CreateAgent(id, m.tenantID, slug)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("creating agent %s: %w", slug, err)
		}
	}

	handle := &AgentHandle{
		ID:       id,
		ParentID: parentID,
		Slug:     slug,
		Status:   "running",
		Cancel:   cancel,
		Runner:   runner,
		ResultCh: make(chan *StreamResult, 64),
	}

	m.agents[id] = handle

	slog.Info("spawned sub-agent",
		"agent_id", id,
		"parent_id", parentID,
		"slug", slug,
	)

	_ = ctx // used by the agent goroutine

	return handle, nil
}

// SendMessage sends a message to a sub-agent and collects the response.
func (m *Manager) SendMessage(ctx context.Context, agentID uuid.UUID, content, sessionKey string) (string, error) {
	m.mu.RLock()
	handle, ok := m.agents[agentID]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("agent %s not found", agentID)
	}

	if handle.Runner == nil {
		return "", fmt.Errorf("agent %s has no runner", agentID)
	}

	resultCh, err := handle.Runner.ProcessMessage(ctx, content, sessionKey)
	if err != nil {
		return "", fmt.Errorf("processing message: %w", err)
	}

	// Collect all text chunks into a single response
	var response string
	for result := range resultCh {
		switch result.Type {
		case "text":
			response += result.Content
		case "error":
			return response, fmt.Errorf("agent error: %s", result.Error)
		case "done":
			return response, nil
		}
	}

	return response, nil
}

// FindBySlug returns a running agent handle by slug.
func (m *Manager) FindBySlug(parentID uuid.UUID, slug string) *AgentHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, h := range m.agents {
		if h.Slug == slug && h.ParentID == parentID && h.Status == "running" {
			return h
		}
	}
	return nil
}

// Stop terminates a running agent.
func (m *Manager) Stop(id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	handle, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("agent %s not found", id)
	}

	if handle.Runner != nil {
		handle.Runner.Shutdown(context.Background())
	}
	handle.Cancel()
	handle.Status = "stopped"
	close(handle.ResultCh)
	delete(m.agents, id)
	return nil
}

// List returns all running agents.
func (m *Manager) List() []*AgentHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*AgentHandle, 0, len(m.agents))
	for _, h := range m.agents {
		result = append(result, h)
	}
	return result
}

// Children returns all child agents of a given parent.
func (m *Manager) Children(parentID uuid.UUID) []*AgentHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var children []*AgentHandle
	for _, h := range m.agents {
		if h.ParentID == parentID {
			children = append(children, h)
		}
	}
	return children
}

// StopAll terminates all running agents.
func (m *Manager) StopAll() {
	m.mu.Lock()
	handles := make([]*AgentHandle, 0, len(m.agents))
	for _, h := range m.agents {
		handles = append(handles, h)
	}
	m.agents = make(map[uuid.UUID]*AgentHandle)
	m.mu.Unlock()

	for _, h := range handles {
		if h.Runner != nil {
			h.Runner.Shutdown(context.Background())
		}
		h.Cancel()
		close(h.ResultCh)
	}
}

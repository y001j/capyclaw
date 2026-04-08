package herd

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

// MessageRouter handles inter-agent message routing.
type MessageRouter struct {
	mu       sync.RWMutex
	handlers map[uuid.UUID]MessageHandler
	manager  *Manager
}

// MessageHandler processes messages sent to an agent.
type MessageHandler func(ctx context.Context, msg *AgentMessage) error

// AgentMessage represents a message between agents.
type AgentMessage struct {
	FromAgentID uuid.UUID `json:"from_agent_id"`
	ToAgentID   uuid.UUID `json:"to_agent_id"`
	Content     string    `json:"content"`
	Type        string    `json:"type"` // request, response, event
}

// NewMessageRouter creates a new inter-agent message router.
func NewMessageRouter(manager *Manager) *MessageRouter {
	return &MessageRouter{
		handlers: make(map[uuid.UUID]MessageHandler),
		manager:  manager,
	}
}

// Register adds a message handler for an agent.
func (r *MessageRouter) Register(agentID uuid.UUID, handler MessageHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[agentID] = handler
}

// Unregister removes a message handler.
func (r *MessageRouter) Unregister(agentID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handlers, agentID)
}

// Send routes a message to the target agent.
func (r *MessageRouter) Send(ctx context.Context, msg *AgentMessage) error {
	r.mu.RLock()
	handler, ok := r.handlers[msg.ToAgentID]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no handler registered for agent %s", msg.ToAgentID)
	}

	return handler(ctx, msg)
}

// Delegate routes a task from a parent agent to a child agent by slug.
// If no matching child exists, one is spawned automatically.
func (r *MessageRouter) Delegate(ctx context.Context, parentID uuid.UUID, targetSlug, task string) (string, error) {
	slog.Info("delegating task",
		"parent_id", parentID,
		"target_slug", targetSlug,
	)

	// Look for an existing child agent with this slug
	handle := r.manager.FindBySlug(parentID, targetSlug)

	// Spawn if not found
	if handle == nil {
		var err error
		handle, err = r.manager.Spawn(ctx, parentID, targetSlug)
		if err != nil {
			return "", fmt.Errorf("spawning agent %s: %w", targetSlug, err)
		}
	}

	// Send the task to the child agent
	sessionKey := fmt.Sprintf("delegate:%s:%s", parentID, handle.ID)
	response, err := r.manager.SendMessage(ctx, handle.ID, task, sessionKey)
	if err != nil {
		return "", fmt.Errorf("delegating to %s: %w", targetSlug, err)
	}

	return response, nil
}

// Broadcast sends a message to all children of a parent agent.
func (r *MessageRouter) Broadcast(ctx context.Context, parentID uuid.UUID, content string) map[uuid.UUID]error {
	children := r.manager.Children(parentID)
	results := make(map[uuid.UUID]error)

	for _, child := range children {
		err := r.Send(ctx, &AgentMessage{
			FromAgentID: parentID,
			ToAgentID:   child.ID,
			Content:     content,
			Type:        "event",
		})
		results[child.ID] = err
	}

	return results
}

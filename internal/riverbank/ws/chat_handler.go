package ws

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"CapyClaw/internal/burrow"
	"CapyClaw/internal/pond/pebble"
)

// ChatSendParams is the params payload for the chat.send method.
type ChatSendParams struct {
	AgentID    string `json:"agent_id"`
	SessionKey string `json:"session_key"`
	Content    string `json:"content"`
}

// WSHandler implements MessageHandler by dispatching to the pipeline.
type WSHandler struct {
	pipeline    *burrow.Pipeline
	agentRepo   *pebble.AgentRepository
	sessionRepo *pebble.SessionRepository
	seq         atomic.Int64

	// activeStreams tracks cancel functions for in-progress chat streams.
	// Key: "clientID:sessionKey", Value: context.CancelFunc
	activeStreams sync.Map
}

// NewWSHandler creates a new WebSocket message handler.
func NewWSHandler(pipeline *burrow.Pipeline, agentRepo *pebble.AgentRepository, sessionRepo *pebble.SessionRepository) *WSHandler {
	return &WSHandler{
		pipeline:    pipeline,
		agentRepo:   agentRepo,
		sessionRepo: sessionRepo,
	}
}

func (h *WSHandler) nextSeq() int64 {
	return h.seq.Add(1)
}

// HandleChatSend processes a chat.send request through the agent pipeline.
func (h *WSHandler) HandleChatSend(client *Client, req *Request) {
	var params ChatSendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendError(req.ID, ErrCodeBadRequest, "invalid chat.send params")
		return
	}

	if params.Content == "" {
		client.SendError(req.ID, ErrCodeBadRequest, "content is required")
		return
	}

	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendError(req.ID, ErrCodeBadRequest, "invalid agent_id")
		return
	}

	tenantID, _ := uuid.Parse(client.TenantID)

	sessionKey := params.SessionKey
	if sessionKey == "" {
		sessionKey = uuid.New().String()
	}

	// Build pipeline request
	pipelineReq := &burrow.PipelineRequest{
		AgentID:  agentID,
		TenantID: tenantID,
		Message: &burrow.IncomingMessage{
			SessionKey: sessionKey,
			Content:    params.Content,
			Role:       "user",
			UserID:     client.UserID,
		},
	}

	// Load agent config if repo is available
	if h.agentRepo != nil {
		agent, err := h.agentRepo.Get(context.Background(), agentID)
		if err != nil {
			client.SendError(req.ID, ErrCodeNotFound, "agent not found")
			return
		}
		pipelineReq.AgentConfig = agentToConfig(agent)
	}

	// Create a cancellable context for this stream
	streamKey := client.UserID + ":" + client.DeviceID + ":" + sessionKey
	ctx, cancel := context.WithCancel(context.Background())
	h.activeStreams.Store(streamKey, cancel)

	// Execute pipeline
	outputCh, err := h.pipeline.Execute(ctx, pipelineReq)
	if err != nil {
		cancel()
		h.activeStreams.Delete(streamKey)
		client.SendError(req.ID, ErrCodeInternal, "pipeline execution failed: "+err.Error())
		return
	}

	// Acknowledge the request
	client.SendResult(req.ID, map[string]string{
		"status":      "streaming",
		"session_key": sessionKey,
	})

	// Stream chunks as events
	for chunk := range outputCh {
		switch chunk.Type {
		case "text":
			client.SendEvent(EventMessageChunk, map[string]string{
				"content": chunk.Content,
			}, h.nextSeq())
		case "error":
			client.SendEvent(EventAgentError, map[string]string{
				"error": chunk.Error,
			}, h.nextSeq())
		case "done":
			client.SendEvent(EventMessageDone, map[string]string{
				"session_key": sessionKey,
			}, h.nextSeq())
		}
	}

	// Clean up stream tracking
	h.activeStreams.Delete(streamKey)
}

// HandleChatCancel cancels an in-progress chat stream.
func (h *WSHandler) HandleChatCancel(client *Client, req *Request) {
	var params struct {
		SessionKey string `json:"session_key"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendError(req.ID, ErrCodeBadRequest, "invalid cancel params")
		return
	}

	streamKey := client.UserID + ":" + client.DeviceID + ":" + params.SessionKey
	if cancelFn, ok := h.activeStreams.LoadAndDelete(streamKey); ok {
		cancelFn.(context.CancelFunc)()
		client.SendResult(req.ID, map[string]string{"status": "cancelled"})
	} else {
		client.SendResult(req.ID, map[string]string{"status": "no_active_stream"})
	}
}

// HandleSessionGet retrieves a session by ID.
func (h *WSHandler) HandleSessionGet(client *Client, req *Request) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendError(req.ID, ErrCodeBadRequest, "invalid params")
		return
	}

	if h.sessionRepo == nil {
		client.SendError(req.ID, ErrCodeInternal, "session repo not available")
		return
	}

	sessionID, err := uuid.Parse(params.SessionID)
	if err != nil {
		client.SendError(req.ID, ErrCodeBadRequest, "invalid session_id")
		return
	}

	session, err := h.sessionRepo.Get(context.Background(), sessionID)
	if err != nil {
		client.SendError(req.ID, ErrCodeNotFound, "session not found")
		return
	}

	client.SendResult(req.ID, session)
}

// HandleSessionsList lists sessions for an agent.
func (h *WSHandler) HandleSessionsList(client *Client, req *Request) {
	var params struct {
		AgentID string `json:"agent_id"`
		Limit   int    `json:"limit"`
		Offset  int    `json:"offset"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendError(req.ID, ErrCodeBadRequest, "invalid params")
		return
	}

	if h.sessionRepo == nil {
		client.SendError(req.ID, ErrCodeInternal, "session repo not available")
		return
	}

	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendError(req.ID, ErrCodeBadRequest, "invalid agent_id")
		return
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}

	sessions, err := h.sessionRepo.ListByAgent(context.Background(), agentID, limit, params.Offset)
	if err != nil {
		client.SendError(req.ID, ErrCodeInternal, "failed to list sessions")
		return
	}

	client.SendResult(req.ID, sessions)
}

// HandleAgentsList lists all agents for the tenant.
func (h *WSHandler) HandleAgentsList(client *Client, req *Request) {
	if h.agentRepo == nil {
		client.SendError(req.ID, ErrCodeInternal, "agent repo not available")
		return
	}

	tenantID, _ := uuid.Parse(client.TenantID)
	agents, err := h.agentRepo.ListByTenant(context.Background(), tenantID, 50, 0)
	if err != nil {
		client.SendError(req.ID, ErrCodeInternal, "failed to list agents")
		return
	}

	client.SendResult(req.ID, agents)
}

// HandleAgentsGet retrieves an agent by ID.
func (h *WSHandler) HandleAgentsGet(client *Client, req *Request) {
	var params struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendError(req.ID, ErrCodeBadRequest, "invalid params")
		return
	}

	if h.agentRepo == nil {
		client.SendError(req.ID, ErrCodeInternal, "agent repo not available")
		return
	}

	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendError(req.ID, ErrCodeBadRequest, "invalid agent_id")
		return
	}

	agent, err := h.agentRepo.Get(context.Background(), agentID)
	if err != nil {
		client.SendError(req.ID, ErrCodeNotFound, "agent not found")
		return
	}

	client.SendResult(req.ID, agent)
}

// agentToConfig converts a pebble.Agent to a burrow.AgentConfig.
func agentToConfig(a *pebble.Agent) burrow.AgentConfig {
	cfg := burrow.AgentConfig{
		Name:           a.Name,
		Slug:           a.Slug,
		Model:          a.Model,
		FallbackModels: a.FallbackModels,
	}
	if a.SystemPrompt != nil {
		cfg.SystemPrompt = *a.SystemPrompt
	}
	if a.IdentityMD != nil {
		cfg.IdentityMD = *a.IdentityMD
	}
	if a.SoulMD != nil {
		cfg.SoulMD = *a.SoulMD
	}
	if a.UserMD != nil {
		cfg.UserMD = *a.UserMD
	}
	return cfg
}

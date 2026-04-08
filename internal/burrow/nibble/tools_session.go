package nibble

import (
	"context"
	"encoding/json"
	"fmt"
)

// SessionFunc types for session operations.
// Set at startup to avoid circular dependency.
type (
	SessionListFunc    func(ctx context.Context, agentID string) ([]map[string]any, error)
	SessionHistoryFunc func(ctx context.Context, sessionID string, limit int) ([]map[string]any, error)
	SessionSendFunc    func(ctx context.Context, sessionID, message string) (string, error)
)

var (
	sessionListFn    SessionListFunc
	sessionHistoryFn SessionHistoryFunc
	sessionSendFn    SessionSendFunc
)

// SetSessionFuncs sets the session operation functions.
func SetSessionFuncs(list SessionListFunc, history SessionHistoryFunc, send SessionSendFunc) {
	sessionListFn = list
	sessionHistoryFn = history
	sessionSendFn = send
}

// registerSessionTools registers session management tools.
// group:sessions — sessions_list, sessions_history, sessions_send
func registerSessionTools(r *Registry) {
	r.Register(&ToolDef{
		Name:        "sessions_list",
		Description: "List active sessions for the current agent. Shows session IDs, creation times, and message counts.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Source: "bundled",
	})
	RegisterBuiltin("sessions_list", sessionsListHandler)

	r.Register(&ToolDef{
		Name:        "sessions_history",
		Description: "Get message history from a specific session. Useful for reviewing past conversations.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"session_id": map[string]any{
					"type":        "string",
					"description": "The session ID to retrieve history for",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of messages to return (default: 20)",
				},
			},
			"required": []string{"session_id"},
		},
		Source: "bundled",
	})
	RegisterBuiltin("sessions_history", sessionsHistoryHandler)

	r.Register(&ToolDef{
		Name:        "sessions_send",
		Description: "Send a message to another session. Enables cross-session communication and sub-agent delegation.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"session_id": map[string]any{
					"type":        "string",
					"description": "Target session ID",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "The message to send",
				},
			},
			"required": []string{"session_id", "message"},
		},
		Source: "bundled",
	})
	RegisterBuiltin("sessions_send", sessionsSendHandler)
}

func sessionsListHandler(ctx context.Context, input map[string]any) (string, error) {
	if sessionListFn == nil {
		return "", fmt.Errorf("session management not available")
	}
	agentID, _ := input["_agent_id"].(string)
	results, err := sessionListFn(ctx, agentID)
	if err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	return string(data), nil
}

func sessionsHistoryHandler(ctx context.Context, input map[string]any) (string, error) {
	if sessionHistoryFn == nil {
		return "", fmt.Errorf("session management not available")
	}
	sessionID, _ := input["session_id"].(string)
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}
	limit := 20
	if v, ok := input["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	results, err := sessionHistoryFn(ctx, sessionID, limit)
	if err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	return string(data), nil
}

func sessionsSendHandler(ctx context.Context, input map[string]any) (string, error) {
	if sessionSendFn == nil {
		return "", fmt.Errorf("session management not available")
	}
	sessionID, _ := input["session_id"].(string)
	message, _ := input["message"].(string)
	if sessionID == "" || message == "" {
		return "", fmt.Errorf("session_id and message are required")
	}
	return sessionSendFn(ctx, sessionID, message)
}

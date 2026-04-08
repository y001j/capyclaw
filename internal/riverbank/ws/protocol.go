package ws

import "encoding/json"

// Frame types for the WebSocket JSON-RPC protocol.
const (
	FrameTypeRequest  = "req"
	FrameTypeResponse = "res"
	FrameTypeEvent    = "event"
)

// Request is a client-to-server JSON-RPC request.
type Request struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is a server-to-client JSON-RPC response.
type Response struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *ErrorPayload   `json:"error,omitempty"`
}

// Event is an unsolicited server-to-client event.
type Event struct {
	Type    string          `json:"type"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Seq     int64           `json:"seq"`
}

// ErrorPayload contains error details in a response.
type ErrorPayload struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

// Protocol error codes.
const (
	ErrCodeBadRequest        = 4000
	ErrCodeRateLimited       = 4001
	ErrCodeForbidden         = 4003
	ErrCodeNotFound          = 4004
	ErrCodeAuthRequired      = 4010
	ErrCodeAuthFailed        = 4011
	ErrCodeDeviceNotPaired   = 4012
	ErrCodeTenantSuspended   = 4020
	ErrCodeBudgetExceeded    = 4030
	ErrCodeInternal          = 5000
	ErrCodeProviderUnavail   = 5001
	ErrCodeSandboxError      = 5002
)

// WebSocket method names.
const (
	MethodConnect         = "connect"
	MethodChatSend        = "chat.send"
	MethodChatCancel      = "chat.cancel"
	MethodSessionsList    = "sessions.list"
	MethodSessionsGet     = "sessions.get"
	MethodSessionsCreate  = "sessions.create"
	MethodSessionsArchive = "sessions.archive"
	MethodAgentsList      = "agents.list"
	MethodAgentsGet       = "agents.get"
	MethodAgentsUpdate    = "agents.update"
	MethodToolsApprove    = "tools.approve"
	MethodToolsReject     = "tools.reject"
	MethodToolsList       = "tools.list"
)

// Event names.
const (
	EventConnectChallenge = "connect.challenge"
	EventMessageChunk     = "agent.message.chunk"
	EventMessageDone      = "agent.message.done"
	EventToolRequest      = "agent.tool.request"
	EventToolResult       = "agent.tool.result"
	EventAgentError       = "agent.error"
	EventSessionUpdated   = "session.updated"
	EventPresenceUpdate   = "presence.update"
)

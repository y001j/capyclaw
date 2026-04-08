// Package protocol defines the WebSocket JSON-RPC 2.0 frame types used for
// communication between the Meadow frontend and the Riverbank gateway.
package protocol

// MessageType distinguishes the three frame categories on the wire.
type MessageType string

const (
	TypeRequest      MessageType = "request"
	TypeResponse     MessageType = "response"
	TypeEvent        MessageType = "event"
	TypeError        MessageType = "error"
)

// Request is a client-to-server JSON-RPC 2.0 call.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Response is a server-to-client reply to a Request.
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Event is a server-initiated push message (not a reply to a specific request).
type Event struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"` // e.g. "agent.thinking", "agent.tool_call"
	Params  any    `json:"params,omitempty"`
}

// Error represents a JSON-RPC 2.0 error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

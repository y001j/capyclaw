package protocol

// Method name constants for JSON-RPC methods used in the WebSocket protocol.
const (
	// Client → Server
	MethodChatSend        = "chat.send"
	MethodChatInterrupt   = "chat.interrupt"
	MethodAgentCreate     = "agent.create"
	MethodAgentUpdate     = "agent.update"
	MethodAgentDelete     = "agent.delete"
	MethodSessionCreate   = "session.create"
	MethodSessionResume   = "session.resume"
	MethodToolApprove     = "tool.approve"
	MethodToolDeny        = "tool.deny"

	// Server → Client (Events)
	EventAgentThinking    = "agent.thinking"
	EventAgentToolCall    = "agent.tool_call"
	EventAgentToolResult  = "agent.tool_result"
	EventAgentMessage     = "agent.message"
	EventAgentDone        = "agent.done"
	EventAgentError       = "agent.error"
	EventSessionCreated   = "session.created"
	EventSessionUpdated   = "session.updated"
	EventMemoryUpdated    = "memory.updated"
	EventSystemHealth     = "system.health"
)

package protocol

// JSON-RPC 2.0 standard error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// CapyClaw application-level error codes (in the -32000 to -32099 range).
const (
	CodeUnauthorized      = -32001
	CodeForbidden         = -32002
	CodeSessionNotFound   = -32003
	CodeAgentNotFound     = -32004
	CodeToolDenied        = -32005
	CodeRateLimitExceeded = -32006
	CodeProviderError     = -32007
	CodeSandboxError      = -32008
)

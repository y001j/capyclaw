package ws

// HandshakeParams represents the client's connection request.
type HandshakeParams struct {
	ProtocolVersion int          `json:"protocol_version"`
	Client          ClientInfo   `json:"client"`
	Role            string       `json:"role"`
	Scopes          []string     `json:"scopes"`
	Auth            AuthInfo     `json:"auth"`
	Device          DeviceInfo   `json:"device"`
}

type ClientInfo struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
}

type AuthInfo struct {
	Method string `json:"method"`
	Token  string `json:"token"`
}

type DeviceInfo struct {
	ID        string `json:"id"`
	Signature string `json:"signature"`
}

// HandshakeResult is sent to the client after successful authentication.
type HandshakeResult struct {
	ProtocolVersion int        `json:"protocol_version"`
	User            UserInfo   `json:"user"`
	Tenant          TenantInfo `json:"tenant"`
	TickIntervalMS  int        `json:"tick_interval_ms"`
}

type UserInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type TenantInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ChallengePayload is sent by the server to initiate handshake.
type ChallengePayload struct {
	Nonce            string `json:"nonce"`
	ProtocolVersions []int  `json:"protocol_versions"`
}

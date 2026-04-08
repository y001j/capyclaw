package burrow

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"

	"CapyClaw/internal/burrow/nibble"
	"CapyClaw/pkg/mcp"
)

// MCPServerConfig describes how to connect to an MCP server.
type MCPServerConfig struct {
	ServerID string `json:"server_id"`
	Type     string `json:"type"` // "stdio" or "sse"
	Command  string `json:"command,omitempty"`
	Args     []string `json:"args,omitempty"`
	URL      string `json:"url,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
}

// MCPConnection represents a live connection to an MCP server.
type MCPConnection struct {
	client   *mcp.Client
	serverID string
	tools    []mcp.ToolDefinition
	lastSeen time.Time
	cmd      *exec.Cmd // only for stdio transport
}

// MCPConnectionManager manages per-agent MCP server connections.
type MCPConnectionManager struct {
	mu          sync.RWMutex
	connections map[uuid.UUID][]*MCPConnection // keyed by agent_id
	registry    *nibble.Registry
}

// NewMCPConnectionManager creates a new MCP connection manager.
func NewMCPConnectionManager(registry *nibble.Registry) *MCPConnectionManager {
	return &MCPConnectionManager{
		connections: make(map[uuid.UUID][]*MCPConnection),
		registry:    registry,
	}
}

// Connect establishes a connection to an MCP server for an agent.
func (m *MCPConnectionManager) Connect(ctx context.Context, agentID uuid.UUID, cfg MCPServerConfig) error {
	var transport mcp.Transport
	var cmd *exec.Cmd

	switch cfg.Type {
	case "stdio":
		cmd = exec.CommandContext(ctx, cfg.Command, cfg.Args...)
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("creating stdin pipe: %w", err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("creating stdout pipe: %w", err)
		}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting MCP server process: %w", err)
		}
		transport = mcp.NewStdioTransport(stdin, stdout)

	case "sse":
		var err error
		transport, err = mcp.NewSSETransport(ctx, cfg.URL)
		if err != nil {
			return fmt.Errorf("connecting SSE transport: %w", err)
		}

	default:
		return fmt.Errorf("unsupported transport type: %s", cfg.Type)
	}

	client, err := mcp.NewClient(ctx, transport)
	if err != nil {
		if cmd != nil {
			cmd.Process.Kill()
		}
		transport.Close()
		return fmt.Errorf("MCP handshake failed: %w", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		client.Close()
		if cmd != nil {
			cmd.Process.Kill()
		}
		return fmt.Errorf("listing MCP tools: %w", err)
	}

	// Register MCP tools in the nibble registry
	for _, tool := range tools {
		m.registry.Register(&nibble.ToolDef{
			Name:        "mcp:" + cfg.ServerID + ":" + tool.Name,
			Description: tool.Description,
			Parameters:  tool.InputSchema,
			Source:      "mcp",
			MCPServerID: cfg.ServerID,
			MCPToolName: tool.Name,
		})
	}

	conn := &MCPConnection{
		client:   client,
		serverID: cfg.ServerID,
		tools:    tools,
		lastSeen: time.Now(),
		cmd:      cmd,
	}

	m.mu.Lock()
	m.connections[agentID] = append(m.connections[agentID], conn)
	m.mu.Unlock()

	slog.Info("connected MCP server",
		"agent_id", agentID,
		"server_id", cfg.ServerID,
		"tools_count", len(tools),
	)

	return nil
}

// Disconnect removes an MCP server connection for an agent.
func (m *MCPConnectionManager) Disconnect(agentID uuid.UUID, serverID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conns, ok := m.connections[agentID]
	if !ok {
		return fmt.Errorf("no connections for agent %s", agentID)
	}

	for i, conn := range conns {
		if conn.serverID == serverID {
			// Unregister tools from registry
			for _, tool := range conn.tools {
				m.registry.Unregister("mcp:" + serverID + ":" + tool.Name)
			}
			conn.client.Close()
			if conn.cmd != nil {
				conn.cmd.Process.Kill()
			}
			m.connections[agentID] = append(conns[:i], conns[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("connection %s not found for agent %s", serverID, agentID)
}

// CallTool invokes a tool on the appropriate MCP server.
func (m *MCPConnectionManager) CallTool(ctx context.Context, agentID uuid.UUID, serverID, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	m.mu.RLock()
	conns := m.connections[agentID]
	m.mu.RUnlock()

	for _, conn := range conns {
		if conn.serverID == serverID {
			conn.lastSeen = time.Now()
			return conn.client.CallTool(ctx, mcp.ToolCall{
				Name:      toolName,
				Arguments: args,
			})
		}
	}

	return nil, fmt.Errorf("MCP server %s not found for agent %s", serverID, agentID)
}

// GetTools returns all MCP tools available for an agent.
func (m *MCPConnectionManager) GetTools(agentID uuid.UUID) []mcp.ToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tools []mcp.ToolDefinition
	for _, conn := range m.connections[agentID] {
		tools = append(tools, conn.tools...)
	}
	return tools
}

// DisconnectAll closes all connections for an agent.
func (m *MCPConnectionManager) DisconnectAll(agentID uuid.UUID) {
	m.mu.Lock()
	conns := m.connections[agentID]
	delete(m.connections, agentID)
	m.mu.Unlock()

	for _, conn := range conns {
		for _, tool := range conn.tools {
			m.registry.Unregister("mcp:" + conn.serverID + ":" + tool.Name)
		}
		conn.client.Close()
		if conn.cmd != nil {
			conn.cmd.Process.Kill()
		}
	}
}

// Close shuts down all MCP connections.
func (m *MCPConnectionManager) Close() error {
	m.mu.Lock()
	allConns := m.connections
	m.connections = make(map[uuid.UUID][]*MCPConnection)
	m.mu.Unlock()

	for _, conns := range allConns {
		for _, conn := range conns {
			conn.client.Close()
			if conn.cmd != nil {
				conn.cmd.Process.Kill()
			}
		}
	}
	return nil
}

// Ensure io is used (stdin pipe implements io.WriteCloser)
var _ io.Closer = (*MCPConnectionManager)(nil)

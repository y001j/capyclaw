package nibble

import "sync"

// ToolDef defines a tool's metadata and capabilities.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	SandboxTier SandboxTier    `json:"sandbox_tier"`
	Source      string         `json:"source"` // bundled, capyhub, workspace, mcp

	// MCP-specific fields (populated when Source == "mcp")
	MCPServerID string `json:"mcp_server_id,omitempty"`
	MCPToolName string `json:"mcp_tool_name,omitempty"`
}

// Registry manages available tools and their definitions.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*ToolDef
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*ToolDef),
	}
}

// Register adds a tool definition to the registry.
func (r *Registry) Register(def *ToolDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[def.Name] = def
}

// Get retrieves a tool definition by name.
func (r *Registry) Get(name string) (*ToolDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.tools[name]
	return def, ok
}

// List returns all registered tools.
func (r *Registry) List() []*ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*ToolDef, 0, len(r.tools))
	for _, def := range r.tools {
		result = append(result, def)
	}
	return result
}

// Unregister removes a tool from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

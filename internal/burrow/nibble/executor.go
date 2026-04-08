package nibble

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"CapyClaw/internal/burrow/mudbath"
)

// SandboxTier defines the isolation level for tool execution.
type SandboxTier string

const (
	TierWASM        SandboxTier = "wasm"
	TierGVisor      SandboxTier = "gvisor"
	TierFirecracker SandboxTier = "firecracker"
)

// ToolCall represents a request to execute a tool.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResult contains the outcome of a tool execution.
type ToolResult struct {
	CallID      string        `json:"call_id"`
	Output      string        `json:"output"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration_ms"`
	SandboxTier SandboxTier   `json:"sandbox_tier"`
}

// MCPCaller is the interface for calling MCP tools.
// This avoids circular imports with the burrow package.
type MCPCaller interface {
	CallTool(ctx context.Context, serverID, toolName string, args map[string]any) (string, error)
}

// Executor dispatches tool calls to the appropriate sandbox tier.
type Executor struct {
	registry  *Registry
	timeout   time.Duration
	sandboxes map[mudbath.Tier]mudbath.Sandbox
	mu        sync.RWMutex
	mcpCaller MCPCaller
}

// NewExecutor creates a new tool executor.
func NewExecutor(registry *Registry, timeout time.Duration) *Executor {
	return &Executor{
		registry:  registry,
		timeout:   timeout,
		sandboxes: make(map[mudbath.Tier]mudbath.Sandbox),
	}
}

// SetMCPCaller sets the MCP caller for routing MCP tool calls.
func (e *Executor) SetMCPCaller(caller MCPCaller) {
	e.mcpCaller = caller
}

// Registry returns the tool registry.
func (e *Executor) Registry() *Registry {
	return e.registry
}

// getSandbox returns the sandbox for the given tier, creating it lazily.
func (e *Executor) getSandbox(tier mudbath.Tier) (mudbath.Sandbox, error) {
	e.mu.RLock()
	sb, ok := e.sandboxes[tier]
	e.mu.RUnlock()
	if ok {
		return sb, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock
	if sb, ok := e.sandboxes[tier]; ok {
		return sb, nil
	}

	sb, err := mudbath.NewSandbox(tier)
	if err != nil {
		return nil, fmt.Errorf("creating sandbox tier %d: %w", tier, err)
	}
	e.sandboxes[tier] = sb
	return sb, nil
}

// tierFromString converts a SandboxTier string to a mudbath.Tier.
func tierFromString(tier SandboxTier) mudbath.Tier {
	switch tier {
	case TierWASM:
		return mudbath.TierWASM
	case TierGVisor:
		return mudbath.TierGVisor
	case TierFirecracker:
		return mudbath.TierFirecracker
	default:
		return mudbath.TierWASM
	}
}

// Execute runs a tool call in the appropriate sandbox or via MCP.
func (e *Executor) Execute(ctx context.Context, call *ToolCall, tier SandboxTier) (*ToolResult, error) {
	start := time.Now()

	// Check if this is an MCP tool
	toolDef, _ := e.registry.Get(call.Name)
	if toolDef != nil && toolDef.Source == "mcp" {
		return e.executeMCP(ctx, call, toolDef, start)
	}

	// Check if this is a built-in tool (executed in-process, no sandbox)
	if handler, ok := GetBuiltin(call.Name); ok {
		slog.Info("executing built-in tool", "tool", call.Name, "call_id", call.ID)
		ctx, cancel := context.WithTimeout(ctx, e.timeout)
		defer cancel()

		output, err := handler(ctx, call.Input)
		if err != nil {
			return &ToolResult{
				CallID:   call.ID,
				Error:    err.Error(),
				Duration: time.Since(start),
			}, nil
		}
		return &ToolResult{
			CallID:   call.ID,
			Output:   output,
			Duration: time.Since(start),
		}, nil
	}

	slog.Info("executing tool",
		"tool", call.Name,
		"tier", tier,
		"call_id", call.ID,
	)

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// Get or create the sandbox for this tier
	sandbox, err := e.getSandbox(tierFromString(tier))
	if err != nil {
		return &ToolResult{
			CallID:      call.ID,
			Error:       fmt.Sprintf("sandbox initialization failed: %v", err),
			Duration:    time.Since(start),
			SandboxTier: tier,
		}, nil
	}

	// Build the execution request
	inputJSON, _ := json.Marshal(call.Input)
	execReq := &mudbath.ExecRequest{
		Command:    call.Name,
		Args:       []string{string(inputJSON)},
		TimeoutSec: int(e.timeout.Seconds()),
		MemoryMB:   256,
	}

	// Execute in sandbox
	result, err := sandbox.Execute(ctx, execReq)
	if err != nil {
		return &ToolResult{
			CallID:      call.ID,
			Error:       fmt.Sprintf("execution failed: %v", err),
			Duration:    time.Since(start),
			SandboxTier: tier,
		}, nil
	}

	// Build result
	toolResult := &ToolResult{
		CallID:      call.ID,
		Output:      result.Stdout,
		Duration:    time.Since(start),
		SandboxTier: tier,
	}
	if result.Stderr != "" {
		toolResult.Error = result.Stderr
	}
	if result.Error != "" {
		toolResult.Error = result.Error
	}

	return toolResult, nil
}

// executeMCP routes a tool call to the appropriate MCP server.
func (e *Executor) executeMCP(ctx context.Context, call *ToolCall, def *ToolDef, start time.Time) (*ToolResult, error) {
	if e.mcpCaller == nil {
		return &ToolResult{
			CallID: call.ID,
			Error:  "MCP caller not configured",
		}, nil
	}

	slog.Info("executing MCP tool",
		"tool", call.Name,
		"server_id", def.MCPServerID,
		"mcp_tool", def.MCPToolName,
		"call_id", call.ID,
	)

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	output, err := e.mcpCaller.CallTool(ctx, def.MCPServerID, def.MCPToolName, call.Input)
	if err != nil {
		return &ToolResult{
			CallID:   call.ID,
			Error:    fmt.Sprintf("MCP call failed: %v", err),
			Duration: time.Since(start),
		}, nil
	}

	return &ToolResult{
		CallID:   call.ID,
		Output:   output,
		Duration: time.Since(start),
	}, nil
}

// Close releases all sandbox resources.
func (e *Executor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []error
	for tier, sb := range e.sandboxes {
		if err := sb.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing tier %d: %w", tier, err))
		}
	}
	e.sandboxes = make(map[mudbath.Tier]mudbath.Sandbox)

	if len(errs) > 0 {
		return fmt.Errorf("closing sandboxes: %v", errs)
	}
	return nil
}

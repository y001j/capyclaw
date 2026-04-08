package mudbath

import (
	"context"
	"fmt"
)

// Tier defines the sandbox isolation level.
type Tier int

const (
	TierWASM Tier = iota + 1
	TierGVisor
	TierFirecracker
)

// Sandbox is the interface for all sandbox implementations.
type Sandbox interface {
	// Execute runs code in the sandbox and returns the output.
	Execute(ctx context.Context, req *ExecRequest) (*ExecResult, error)

	// Tier returns the sandbox tier.
	Tier() Tier

	// Close releases sandbox resources.
	Close() error
}

// ExecRequest contains the code or command to execute in a sandbox.
type ExecRequest struct {
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Stdin      string            `json:"stdin,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	WorkDir    string            `json:"work_dir,omitempty"`
	TimeoutSec int               `json:"timeout_sec"`
	MemoryMB   int               `json:"memory_mb"`
}

// ExecResult contains the output of a sandbox execution.
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// SelectTier determines the appropriate sandbox tier for a given source.
func SelectTier(source string, verified bool) Tier {
	switch {
	case source == "bundled" || (source == "capyhub" && verified):
		return TierWASM
	case source == "workspace" || source == "capyhub":
		return TierGVisor
	default:
		return TierFirecracker
	}
}

// NewSandbox creates a sandbox of the specified tier.
func NewSandbox(tier Tier) (Sandbox, error) {
	switch tier {
	case TierWASM:
		return NewWASMSandbox()
	case TierGVisor:
		return NewGVisorSandbox()
	case TierFirecracker:
		return NewFirecrackerSandbox() // Use WithKernel/WithVMResources for configured instances
	default:
		return nil, fmt.Errorf("unknown sandbox tier: %d", tier)
	}
}

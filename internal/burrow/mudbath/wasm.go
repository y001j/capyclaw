package mudbath

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// WASMConfig holds configuration for the WASM sandbox.
type WASMConfig struct {
	MaxMemoryPages  uint32        // Each page is 64KB
	MaxExecDuration time.Duration // Maximum execution time
}

// DefaultWASMConfig returns sensible defaults for WASM execution.
func DefaultWASMConfig() WASMConfig {
	return WASMConfig{
		MaxMemoryPages:  256, // 16MB
		MaxExecDuration: 30 * time.Second,
	}
}

// WASMSandbox implements Tier 1 sandboxing via Wazero (pure Go WASM runtime).
type WASMSandbox struct {
	runtime wazero.Runtime
	config  WASMConfig
}

// NewWASMSandbox creates a new WASM sandbox with the default configuration.
func NewWASMSandbox() (*WASMSandbox, error) {
	return NewWASMSandboxWithConfig(DefaultWASMConfig())
}

// NewWASMSandboxWithConfig creates a new WASM sandbox with the given configuration.
func NewWASMSandboxWithConfig(cfg WASMConfig) (*WASMSandbox, error) {
	ctx := context.Background()

	// Create Wazero runtime with memory limits
	runtimeConfig := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(cfg.MaxMemoryPages)

	rt := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Register WASI for standard I/O (no filesystem, no network by default)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	return &WASMSandbox{
		runtime: rt,
		config:  cfg,
	}, nil
}

func (s *WASMSandbox) Tier() Tier { return TierWASM }

func (s *WASMSandbox) Execute(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
	// Set execution timeout
	timeout := s.config.MaxExecDuration
	if req.TimeoutSec > 0 {
		timeout = time.Duration(req.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The Command field should contain the path to the WASM module bytes.
	// In a real implementation, we'd load the WASM bytes from a registry or filesystem.
	// For now, we expect the WASM bytes to be passed via Stdin as base64 or from a cached module.
	wasmBytes := []byte(req.Stdin)
	if len(wasmBytes) == 0 {
		return &ExecResult{
			ExitCode: 1,
			Error:    "no WASM module provided",
		}, nil
	}

	// Compile the WASM module
	compiled, err := s.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return &ExecResult{
			ExitCode: 1,
			Error:    fmt.Sprintf("compiling WASM module: %v", err),
		}, nil
	}
	defer compiled.Close(ctx)

	// Configure module: capture stdout/stderr, set args and env
	var stdout, stderr bytes.Buffer
	modConfig := wazero.NewModuleConfig().
		WithStdout(&stdout).
		WithStderr(&stderr).
		WithName(req.Command)

	// Set arguments
	if len(req.Args) > 0 {
		modConfig = modConfig.WithArgs(append([]string{req.Command}, req.Args...)...)
	} else {
		modConfig = modConfig.WithArgs(req.Command)
	}

	// Set environment variables
	for k, v := range req.Env {
		modConfig = modConfig.WithEnv(k, v)
	}

	// No filesystem mount — WASM modules run in complete isolation

	// Instantiate and run the module
	mod, err := s.runtime.InstantiateModule(ctx, compiled, modConfig)
	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			return &ExecResult{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				ExitCode: -1,
				Error:    "execution timed out",
			}, nil
		}
		return &ExecResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: 1,
			Error:    fmt.Sprintf("executing WASM module: %v", err),
		}, nil
	}
	defer mod.Close(ctx)

	return &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}, nil
}

func (s *WASMSandbox) Close() error {
	return s.runtime.Close(context.Background())
}

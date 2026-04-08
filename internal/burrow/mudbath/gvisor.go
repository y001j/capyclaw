package mudbath

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// GVisorSandbox implements Tier 2 sandboxing via gVisor (runsc).
// It creates OCI-compatible containers with syscall interception,
// namespace isolation, and restricted filesystem access.
type GVisorSandbox struct {
	runtimeClass string
	maxMemoryMB  int
	maxCPU       int
	available    bool
}

// NewGVisorSandbox creates a new gVisor sandbox.
func NewGVisorSandbox() (*GVisorSandbox, error) {
	s := &GVisorSandbox{
		runtimeClass: "runsc",
		maxMemoryMB:  512,
		maxCPU:       500,
	}

	// Check if runsc is available
	if _, err := exec.LookPath("runsc"); err != nil {
		slog.Warn("gVisor runsc not found, sandbox will run in degraded mode", "error", err)
		s.available = false
	} else {
		s.available = true
	}

	return s, nil
}

func (s *GVisorSandbox) Tier() Tier { return TierGVisor }

func (s *GVisorSandbox) Execute(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
	if !s.available {
		return s.executeFallback(ctx, req)
	}

	containerID := fmt.Sprintf("capyclaw-gvisor-%d", time.Now().UnixNano())

	// Create temporary OCI bundle directory
	bundleDir, err := os.MkdirTemp("", "capyclaw-oci-*")
	if err != nil {
		return nil, fmt.Errorf("creating bundle dir: %w", err)
	}
	defer os.RemoveAll(bundleDir)

	rootfsDir := filepath.Join(bundleDir, "rootfs")
	if err := os.MkdirAll(rootfsDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating rootfs dir: %w", err)
	}

	// Write OCI config.json
	memLimitBytes := int64(s.maxMemoryMB) * 1024 * 1024
	if req.MemoryMB > 0 {
		memLimitBytes = int64(req.MemoryMB) * 1024 * 1024
	}

	ociConfig := map[string]any{
		"ociVersion": "1.0.2",
		"process": map[string]any{
			"terminal": false,
			"user":     map[string]any{"uid": 65534, "gid": 65534},
			"args":     append([]string{req.Command}, req.Args...),
			"env":      buildEnvList(req.Env),
			"cwd":      "/",
		},
		"root": map[string]any{
			"path":     "rootfs",
			"readonly": true,
		},
		"linux": map[string]any{
			"namespaces": []map[string]string{
				{"type": "pid"},
				{"type": "network"},
				{"type": "mount"},
				{"type": "ipc"},
				{"type": "uts"},
			},
			"resources": map[string]any{
				"memory": map[string]any{
					"limit": memLimitBytes,
				},
				"cpu": map[string]any{
					"quota":  int64(s.maxCPU) * 100,
					"period": 100000,
				},
			},
		},
		"mounts": []map[string]any{
			{
				"destination": "/tmp",
				"type":        "tmpfs",
				"source":      "tmpfs",
				"options":     []string{"nosuid", "nodev", "size=64m"},
			},
			{
				"destination": "/proc",
				"type":        "proc",
				"source":      "proc",
			},
		},
	}

	configJSON, err := json.Marshal(ociConfig)
	if err != nil {
		return nil, fmt.Errorf("marshaling OCI config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "config.json"), configJSON, 0o644); err != nil {
		return nil, fmt.Errorf("writing config.json: %w", err)
	}

	// Set up timeout
	timeout := time.Duration(req.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Run the container with gVisor
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "runsc",
		"run",
		"--rootless",
		"--network=none",
		"--bundle", bundleDir,
		containerID,
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	slog.Debug("executing in gVisor sandbox",
		"container_id", containerID,
		"command", req.Command,
	)

	err = cmd.Run()

	result := &ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "execution timed out"
			result.ExitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.Error = err.Error()
			result.ExitCode = -1
		}
	}

	// Clean up container
	cleanupCmd := exec.Command("runsc", "delete", "--force", containerID)
	cleanupCmd.Run()

	return result, nil
}

// executeFallback runs the command with the best available isolation when runsc is unavailable.
// On macOS, it uses sandbox-exec (Seatbelt) for process-level sandboxing.
// On other platforms, it falls back to bare process execution with a warning.
func (s *GVisorSandbox) executeFallback(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
	// Try macOS sandbox first
	darwin, err := NewDarwinSandbox()
	if err == nil && darwin.Available() {
		slog.Info("gVisor unavailable, using macOS sandbox-exec fallback",
			"command", req.Command,
		)
		return darwin.Execute(ctx, req)
	}

	slog.Warn("running in fallback mode without any sandbox isolation",
		"command", req.Command,
	)

	timeout := time.Duration(req.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, req.Command, req.Args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = req.WorkDir

	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	err = cmd.Run()

	result := &ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "execution timed out"
			result.ExitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.Error = err.Error()
			result.ExitCode = -1
		}
	}

	return result, nil
}

func (s *GVisorSandbox) Close() error {
	return nil
}

// buildEnvList converts a map to OCI environment format.
func buildEnvList(env map[string]string) []string {
	result := []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

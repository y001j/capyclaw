package mudbath

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"time"
)

// seatbeltProfile is a macOS Sandbox (Seatbelt) profile that restricts:
// - All network access (inbound and outbound)
// - File writes to sensitive system locations
// - Signal sending to other processes
//
// It allows broad file reads (needed for dynamic linker, shared caches, etc.)
// and writes only to temporary directories and /dev/null.
const seatbeltProfile = `
(version 1)
(deny default)

;; Allow basic process execution and forking
(allow process-exec*)
(allow process-fork)

;; Allow broad file reads — programs need access to dyld cache, system libs, etc.
(allow file-read*)

;; Allow sysctl reads (needed by many programs)
(allow sysctl-read)

;; Allow mach lookups (needed for basic IPC with system services)
(allow mach-lookup)

;; Allow writes to temporary directories and /dev/null only
(allow file-write*
    (literal "/dev/null")
    (subpath "/private/tmp")
    (subpath "/tmp")
    (regex #"^/private/var/folders/")
)

;; Deny all network access (inbound and outbound)
(deny network*)

;; Deny signal sending to other processes
(deny signal (target others))

;; Deny writes to sensitive system paths (evaluated after the allow rules above)
(deny file-write*
    (subpath "/etc")
    (subpath "/usr")
    (subpath "/System")
    (subpath "/Library")
    (subpath "/bin")
    (subpath "/sbin")
    (subpath "/var/db")
)
`

// DarwinSandbox implements process-level sandboxing on macOS using sandbox-exec (Seatbelt).
// This provides a reasonable isolation layer for development environments where
// gVisor and Firecracker are unavailable.
type DarwinSandbox struct {
	available bool
}

// NewDarwinSandbox creates a new macOS sandbox.
func NewDarwinSandbox() (*DarwinSandbox, error) {
	s := &DarwinSandbox{}

	if runtime.GOOS != "darwin" {
		s.available = false
		return s, nil
	}

	// Check if sandbox-exec is available
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		slog.Warn("sandbox-exec not found on macOS", "error", err)
		s.available = false
	} else {
		s.available = true
	}

	return s, nil
}

// Available returns whether the macOS sandbox is usable.
func (s *DarwinSandbox) Available() bool {
	return s.available
}

// Execute runs a command inside the macOS sandbox-exec sandbox.
func (s *DarwinSandbox) Execute(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
	if !s.available {
		return nil, fmt.Errorf("macOS sandbox not available")
	}

	timeout := time.Duration(req.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build the sandbox-exec command:
	// sandbox-exec -p '<profile>' <command> [args...]
	args := []string{"-p", seatbeltProfile, req.Command}
	args = append(args, req.Args...)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "sandbox-exec", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if req.WorkDir != "" {
		cmd.Dir = req.WorkDir
	}

	// Set environment variables
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Ensure basic PATH is available
	if _, ok := req.Env["PATH"]; !ok {
		cmd.Env = append(cmd.Env, "PATH=/usr/local/bin:/usr/bin:/bin")
	}

	slog.Debug("executing in macOS sandbox",
		"command", req.Command,
		"args", req.Args,
	)

	err := cmd.Run()

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

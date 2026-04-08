package nibble

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// registerExecTools registers shell execution and process management tools.
// group:runtime — exec, bash
func registerExecTools(r *Registry) {
	// exec: execute a shell command and return output
	r.Register(&ToolDef{
		Name:        "exec",
		Description: "Execute a shell command and return its stdout/stderr. Use for running scripts, CLI tools, build commands, etc. Commands run in the agent's workspace directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The shell command to execute",
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Maximum execution time in seconds (default: 30, max: 300)",
				},
				"working_dir": map[string]any{
					"type":        "string",
					"description": "Working directory for the command (default: agent workspace)",
				},
			},
			"required": []string{"command"},
		},
		SandboxTier: TierWASM,
		Source:      "bundled",
	})
	RegisterBuiltin("exec", execHandler)

	// bash: execute a bash script (multi-line)
	r.Register(&ToolDef{
		Name:        "bash",
		Description: "Execute a multi-line bash script. Useful for complex operations that require multiple commands, loops, or conditionals.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"script": map[string]any{
					"type":        "string",
					"description": "The bash script to execute",
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Maximum execution time in seconds (default: 30, max: 300)",
				},
			},
			"required": []string{"script"},
		},
		SandboxTier: TierWASM,
		Source:      "bundled",
	})
	RegisterBuiltin("bash", bashHandler)
}

func execHandler(ctx context.Context, input map[string]any) (string, error) {
	command, _ := input["command"].(string)
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	timeout := 30
	if v, ok := input["timeout_seconds"].(float64); ok && v > 0 {
		timeout = int(v)
	}
	if timeout > 300 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if dir, ok := input["working_dir"].(string); ok && dir != "" {
		cmd.Dir = dir
	}

	// Prevent shell injection via environment
	cmd.Env = sanitizedEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var result strings.Builder
	if stdout.Len() > 0 {
		result.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if result.Len() > 0 {
			result.WriteString("\n--- stderr ---\n")
		}
		result.WriteString(stderr.String())
	}

	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			}
		}
		if result.Len() == 0 {
			result.WriteString(err.Error())
		}
		result.WriteString(fmt.Sprintf("\n[exit code: %d]", exitCode))
	}

	// Truncate large output
	output := result.String()
	if len(output) > 50000 {
		output = output[:50000] + "\n... [output truncated at 50000 chars]"
	}

	return output, nil
}

func bashHandler(ctx context.Context, input map[string]any) (string, error) {
	script, _ := input["script"].(string)
	if script == "" {
		return "", fmt.Errorf("script is required")
	}
	// Delegate to exec with bash -c
	return execHandler(ctx, map[string]any{
		"command":         "bash -c " + shellQuote(script),
		"timeout_seconds": input["timeout_seconds"],
	})
}

// sanitizedEnv returns a minimal safe environment.
func sanitizedEnv() []string {
	return []string{
		"PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=/tmp",
		"LANG=en_US.UTF-8",
	}
}

// shellQuote wraps a string in single quotes for safe shell passing.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

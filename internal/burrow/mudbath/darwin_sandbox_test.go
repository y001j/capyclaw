package mudbath

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestDarwinSandbox_Available(t *testing.T) {
	s, err := NewDarwinSandbox()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if runtime.GOOS == "darwin" {
		if !s.Available() {
			t.Log("sandbox-exec not found on this macOS system (unusual but possible)")
		}
	} else {
		if s.Available() {
			t.Error("expected unavailable on non-darwin")
		}
	}
}

func TestDarwinSandbox_Execute(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox only available on darwin")
	}

	s, err := NewDarwinSandbox()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.Available() {
		t.Skip("sandbox-exec not available")
	}

	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "echo",
		Args:       []string{"hello from sandbox"},
		TimeoutSec: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0; stderr: %s", result.ExitCode, result.Stderr)
	}
	if got := strings.TrimSpace(result.Stdout); got != "hello from sandbox" {
		t.Errorf("stdout = %q, want %q", got, "hello from sandbox")
	}
}

func TestDarwinSandbox_EnvVars(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox only available on darwin")
	}

	s, err := NewDarwinSandbox()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.Available() {
		t.Skip("sandbox-exec not available")
	}

	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "sh",
		Args:       []string{"-c", "echo $CAPY_TEST"},
		Env:        map[string]string{"CAPY_TEST": "capybara"},
		TimeoutSec: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.TrimSpace(result.Stdout); got != "capybara" {
		t.Errorf("stdout = %q, want %q", got, "capybara")
	}
}

func TestDarwinSandbox_Timeout(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox only available on darwin")
	}

	s, err := NewDarwinSandbox()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.Available() {
		t.Skip("sandbox-exec not available")
	}

	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "sleep",
		Args:       []string{"10"},
		TimeoutSec: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != -1 {
		t.Errorf("exit code = %d, want -1", result.ExitCode)
	}
	if result.Error != "execution timed out" {
		t.Errorf("error = %q, want %q", result.Error, "execution timed out")
	}
}

func TestDarwinSandbox_NetworkDenied(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox only available on darwin")
	}

	s, err := NewDarwinSandbox()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.Available() {
		t.Skip("sandbox-exec not available")
	}

	// curl should fail because the sandbox denies all network access
	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "curl",
		Args:       []string{"-s", "--max-time", "3", "https://example.com"},
		TimeoutSec: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code when network is denied")
	}
}

func TestDarwinSandbox_FileWriteDenied(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox only available on darwin")
	}

	s, err := NewDarwinSandbox()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.Available() {
		t.Skip("sandbox-exec not available")
	}

	// Writing to a protected location should fail
	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "sh",
		Args:       []string{"-c", "echo hacked > /etc/capyclaw_test_file"},
		TimeoutSec: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code when writing to protected path")
	}
}

func TestDarwinSandbox_BadCommand(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox only available on darwin")
	}

	s, err := NewDarwinSandbox()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.Available() {
		t.Skip("sandbox-exec not available")
	}

	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "nonexistent_command_xyz",
		TimeoutSec: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code for bad command")
	}
}

func TestDarwinSandbox_Unavailable(t *testing.T) {
	s := &DarwinSandbox{available: false}

	_, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "echo",
		Args:       []string{"test"},
		TimeoutSec: 5,
	})
	if err == nil {
		t.Error("expected error when sandbox is unavailable")
	}
}

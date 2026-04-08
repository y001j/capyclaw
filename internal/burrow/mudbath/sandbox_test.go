package mudbath

import (
	"context"
	"runtime"
	"testing"
)

func TestSelectTier(t *testing.T) {
	tests := []struct {
		source   string
		verified bool
		want     Tier
	}{
		{"bundled", false, TierWASM},
		{"bundled", true, TierWASM},
		{"capyhub", true, TierWASM},
		{"capyhub", false, TierGVisor},
		{"workspace", false, TierGVisor},
		{"workspace", true, TierGVisor},
		{"unknown", false, TierFirecracker},
		{"upload", false, TierFirecracker},
	}

	for _, tt := range tests {
		got := SelectTier(tt.source, tt.verified)
		if got != tt.want {
			t.Errorf("SelectTier(%q, %v) = %d, want %d", tt.source, tt.verified, got, tt.want)
		}
	}
}

func TestFirecrackerSandbox_Tier(t *testing.T) {
	s, err := NewFirecrackerSandbox()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Tier() != TierFirecracker {
		t.Errorf("tier = %d, want %d", s.Tier(), TierFirecracker)
	}
}

func TestFirecrackerSandbox_FallbackMode(t *testing.T) {
	s, err := NewFirecrackerSandbox()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// On non-Linux or without KVM, sandbox should be in degraded mode
	if runtime.GOOS != "linux" {
		if s.available {
			t.Error("expected unavailable on non-Linux")
		}
	}
}

func TestFirecrackerSandbox_FallbackExecution(t *testing.T) {
	s := &FirecrackerSandbox{available: false}

	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "echo",
		Args:       []string{"hello world"},
		TimeoutSec: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
	if result.Stdout != "hello world\n" {
		t.Errorf("stdout = %q, want 'hello world\\n'", result.Stdout)
	}
}

func TestFirecrackerSandbox_FallbackTimeout(t *testing.T) {
	s := &FirecrackerSandbox{available: false}

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
		t.Errorf("error = %q, want 'execution timed out'", result.Error)
	}
}

func TestFirecrackerSandbox_FallbackEnv(t *testing.T) {
	s := &FirecrackerSandbox{available: false}

	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "sh",
		Args:       []string{"-c", "echo $MY_VAR"},
		Env:        map[string]string{"MY_VAR": "capybara"},
		TimeoutSec: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stdout != "capybara\n" {
		t.Errorf("stdout = %q, want 'capybara\\n'", result.Stdout)
	}
}

func TestFirecrackerSandbox_FallbackBadCommand(t *testing.T) {
	s := &FirecrackerSandbox{available: false}

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

func TestFirecrackerSandbox_Options(t *testing.T) {
	s, err := NewFirecrackerSandbox(
		WithKernel("/boot/vmlinux", "/rootfs.ext4"),
		WithVMResources(2048, 4),
		WithBootTimeout(10e9), // 10s
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.kernelPath != "/boot/vmlinux" {
		t.Errorf("kernelPath = %s", s.kernelPath)
	}
	if s.rootFSPath != "/rootfs.ext4" {
		t.Errorf("rootFSPath = %s", s.rootFSPath)
	}
	if s.maxMemMB != 2048 {
		t.Errorf("maxMemMB = %d", s.maxMemMB)
	}
	if s.maxVCPUs != 4 {
		t.Errorf("maxVCPUs = %d", s.maxVCPUs)
	}
}

func TestFirecrackerSandbox_Close(t *testing.T) {
	s := &FirecrackerSandbox{}
	if err := s.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

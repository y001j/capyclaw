//go:build linux && integration

package mudbath

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestFirecrackerIntegration_Prerequisites(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("KVM not available: ", err)
	}
	if _, err := exec.LookPath("firecracker"); err != nil {
		t.Skip("firecracker binary not found: ", err)
	}
}

func TestFirecrackerIntegration_Execute(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("KVM not available")
	}
	if _, err := exec.LookPath("firecracker"); err != nil {
		t.Skip("firecracker binary not found")
	}

	kernelPath := os.Getenv("CAPYCLAW_TEST_FC_KERNEL")
	rootfsPath := os.Getenv("CAPYCLAW_TEST_FC_ROOTFS")
	if kernelPath == "" || rootfsPath == "" {
		t.Skip("set CAPYCLAW_TEST_FC_KERNEL and CAPYCLAW_TEST_FC_ROOTFS to run Firecracker integration tests")
	}

	s, err := NewFirecrackerSandbox(
		WithKernel(kernelPath, rootfsPath),
		WithVMResources(512, 1),
	)
	if err != nil {
		t.Fatalf("creating sandbox: %v", err)
	}
	defer s.Close()

	if !s.available {
		t.Fatal("expected sandbox to be available with KVM + firecracker")
	}

	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "/bin/echo",
		Args:       []string{"hello from microVM"},
		TimeoutSec: 30,
	})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0; error: %s; stderr: %s", result.ExitCode, result.Error, result.Stderr)
	}
	if got := strings.TrimSpace(result.Stdout); got != "hello from microVM" {
		t.Errorf("stdout = %q, want %q", got, "hello from microVM")
	}
}

func TestFirecrackerIntegration_NetworkIsolation(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("KVM not available")
	}
	if _, err := exec.LookPath("firecracker"); err != nil {
		t.Skip("firecracker binary not found")
	}

	kernelPath := os.Getenv("CAPYCLAW_TEST_FC_KERNEL")
	rootfsPath := os.Getenv("CAPYCLAW_TEST_FC_ROOTFS")
	if kernelPath == "" || rootfsPath == "" {
		t.Skip("set CAPYCLAW_TEST_FC_KERNEL and CAPYCLAW_TEST_FC_ROOTFS")
	}

	s, err := NewFirecrackerSandbox(
		WithKernel(kernelPath, rootfsPath),
		WithVMResources(512, 1),
	)
	if err != nil {
		t.Fatalf("creating sandbox: %v", err)
	}
	defer s.Close()

	// The microVM has no network interface configured, so curl should fail
	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "/usr/bin/curl",
		Args:       []string{"-s", "--max-time", "3", "https://example.com"},
		TimeoutSec: 15,
	})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code when network is unavailable in microVM")
	}
}

func TestFirecrackerIntegration_Timeout(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("KVM not available")
	}
	if _, err := exec.LookPath("firecracker"); err != nil {
		t.Skip("firecracker binary not found")
	}

	kernelPath := os.Getenv("CAPYCLAW_TEST_FC_KERNEL")
	rootfsPath := os.Getenv("CAPYCLAW_TEST_FC_ROOTFS")
	if kernelPath == "" || rootfsPath == "" {
		t.Skip("set CAPYCLAW_TEST_FC_KERNEL and CAPYCLAW_TEST_FC_ROOTFS")
	}

	s, err := NewFirecrackerSandbox(
		WithKernel(kernelPath, rootfsPath),
		WithVMResources(512, 1),
	)
	if err != nil {
		t.Fatalf("creating sandbox: %v", err)
	}
	defer s.Close()

	result, err := s.Execute(context.Background(), &ExecRequest{
		Command:    "/bin/sleep",
		Args:       []string{"60"},
		TimeoutSec: 2,
	})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if result.Error != "execution timed out" {
		t.Errorf("error = %q, want %q", result.Error, "execution timed out")
	}
}

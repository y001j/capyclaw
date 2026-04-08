package mudbath

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// vsockGuestPort is the well-known vsock port the guest agent listens on.
const vsockGuestPort = 10789

// vsockGuestCID is the guest context ID. CID 3+ are for guests (0=hypervisor, 1=reserved, 2=host).
const vsockGuestCID = 3

// guestCommand is the JSON envelope sent to the guest agent over vsock.
type guestCommand struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Stdin   string            `json:"stdin,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	WorkDir string            `json:"work_dir,omitempty"`
	Timeout int               `json:"timeout"`
}

// guestResult is the JSON response from the guest agent.
type guestResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// FirecrackerSandbox implements Tier 3 sandboxing via Firecracker microVMs.
// Provides full hardware-level isolation using KVM. Only available on Linux.
type FirecrackerSandbox struct {
	kernelPath  string
	rootFSPath  string
	maxMemMB    int
	maxVCPUs    int
	bootTimeout time.Duration
	available   bool
}

// FirecrackerOption configures the Firecracker sandbox.
type FirecrackerOption func(*FirecrackerSandbox)

// WithKernel sets the kernel image and rootfs paths for the microVM.
func WithKernel(kernelPath, rootFSPath string) FirecrackerOption {
	return func(s *FirecrackerSandbox) {
		s.kernelPath = kernelPath
		s.rootFSPath = rootFSPath
	}
}

// WithVMResources sets the memory and vCPU limits.
func WithVMResources(memMB, vcpus int) FirecrackerOption {
	return func(s *FirecrackerSandbox) {
		if memMB > 0 {
			s.maxMemMB = memMB
		}
		if vcpus > 0 {
			s.maxVCPUs = vcpus
		}
	}
}

// WithBootTimeout sets the VM boot timeout.
func WithBootTimeout(d time.Duration) FirecrackerOption {
	return func(s *FirecrackerSandbox) {
		if d > 0 {
			s.bootTimeout = d
		}
	}
}

// NewFirecrackerSandbox creates a new Firecracker microVM sandbox.
func NewFirecrackerSandbox(opts ...FirecrackerOption) (*FirecrackerSandbox, error) {
	s := &FirecrackerSandbox{
		maxMemMB:    1024,
		maxVCPUs:    2,
		bootTimeout: 5 * time.Second,
	}

	for _, opt := range opts {
		opt(s)
	}

	// Firecracker requires Linux + KVM
	if runtime.GOOS != "linux" {
		slog.Warn("Firecracker requires Linux with KVM, sandbox will run in degraded mode",
			"os", runtime.GOOS,
		)
		s.available = false
		return s, nil
	}

	// Check for KVM support
	if _, err := os.Stat("/dev/kvm"); err != nil {
		slog.Warn("KVM not available, Firecracker sandbox will run in degraded mode")
		s.available = false
		return s, nil
	}

	// Check for firecracker binary
	if _, err := exec.LookPath("firecracker"); err != nil {
		slog.Warn("firecracker binary not found, sandbox will run in degraded mode")
		s.available = false
		return s, nil
	}

	s.available = true
	return s, nil
}

func (s *FirecrackerSandbox) Tier() Tier { return TierFirecracker }

func (s *FirecrackerSandbox) Execute(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
	if !s.available {
		return s.executeFallback(ctx, req)
	}

	return s.executeFirecracker(ctx, req)
}

func (s *FirecrackerSandbox) executeFirecracker(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
	// Create temporary directory for this VM instance
	vmDir, err := os.MkdirTemp("", "capyclaw-fc-*")
	if err != nil {
		return nil, fmt.Errorf("creating VM dir: %w", err)
	}
	defer os.RemoveAll(vmDir)

	socketPath := filepath.Join(vmDir, "firecracker.sock")

	// Start Firecracker process
	fcCmd := exec.CommandContext(ctx, "firecracker",
		"--api-sock", socketPath,
	)
	fcCmd.Stdout = os.Stderr // Log Firecracker output
	fcCmd.Stderr = os.Stderr

	if err := fcCmd.Start(); err != nil {
		return nil, fmt.Errorf("starting firecracker: %w", err)
	}
	defer func() {
		fcCmd.Process.Kill()
		fcCmd.Wait()
	}()

	// Wait for socket to become available
	if err := waitForSocket(socketPath, s.bootTimeout); err != nil {
		return nil, fmt.Errorf("waiting for firecracker socket: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}

	// Configure the VM
	memMB := s.maxMemMB
	if req.MemoryMB > 0 && req.MemoryMB < s.maxMemMB {
		memMB = req.MemoryMB
	}

	// Set machine config
	machineConfig := map[string]any{
		"vcpu_count":  s.maxVCPUs,
		"mem_size_mib": memMB,
	}
	if err := fcAPICall(client, "PUT", "/machine-config", machineConfig); err != nil {
		return nil, fmt.Errorf("setting machine config: %w", err)
	}

	// Set boot source
	if s.kernelPath != "" {
		bootSource := map[string]any{
			"kernel_image_path": s.kernelPath,
			"boot_args":        "console=ttyS0 reboot=k panic=1 pci=off",
		}
		if err := fcAPICall(client, "PUT", "/boot-source", bootSource); err != nil {
			return nil, fmt.Errorf("setting boot source: %w", err)
		}
	}

	// Set root drive
	if s.rootFSPath != "" {
		drive := map[string]any{
			"drive_id":       "rootfs",
			"path_on_host":   s.rootFSPath,
			"is_root_device": true,
			"is_read_only":   true,
		}
		if err := fcAPICall(client, "PUT", "/drives/rootfs", drive); err != nil {
			return nil, fmt.Errorf("setting root drive: %w", err)
		}
	}

	// Configure vsock device for guest agent communication
	vsockConfig := map[string]any{
		"guest_cid": vsockGuestCID,
		"uds_path":  filepath.Join(vmDir, "vsock.sock"),
	}
	if err := fcAPICall(client, "PUT", "/vsock", vsockConfig); err != nil {
		return nil, fmt.Errorf("setting vsock: %w", err)
	}

	// Start the VM
	action := map[string]any{"action_type": "InstanceStart"}
	if err := fcAPICall(client, "PUT", "/actions", action); err != nil {
		return nil, fmt.Errorf("starting VM: %w", err)
	}

	slog.Debug("firecracker VM started",
		"socket", socketPath,
		"vcpus", s.maxVCPUs,
		"memory_mb", memMB,
	)

	// Execute the command inside the VM via vsock guest agent
	timeout := time.Duration(req.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	execCtx, execCancel := context.WithTimeout(ctx, timeout)
	defer execCancel()

	result, err := s.executeViaVsock(execCtx, filepath.Join(vmDir, "vsock.sock"), req)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return &ExecResult{Error: "execution cancelled", ExitCode: -1}, nil
		}
		if execCtx.Err() == context.DeadlineExceeded {
			return &ExecResult{Error: "execution timed out", ExitCode: -1}, nil
		}
		return nil, fmt.Errorf("vsock execution: %w", err)
	}

	return result, nil
}

// executeViaVsock connects to the guest agent over the Firecracker vsock UDS
// and sends the command for execution inside the VM.
func (s *FirecrackerSandbox) executeViaVsock(ctx context.Context, vsockUDS string, req *ExecRequest) (*ExecResult, error) {
	// Firecracker exposes guest vsock as a Unix socket on the host.
	// To reach guest CID 3, port P, connect to the UDS and write "CONNECT <port>\n".
	// Wait for the guest agent to be ready (it starts with the VM init).
	var conn net.Conn
	var err error

	retryDeadline := time.Now().Add(s.bootTimeout)
	for time.Now().Before(retryDeadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		conn, err = net.DialTimeout("unix", vsockUDS, 500*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to vsock UDS: %w", err)
	}
	defer conn.Close()

	// Firecracker vsock multiplexing: send CONNECT <port>\n to reach guest agent
	connectMsg := fmt.Sprintf("CONNECT %d\n", vsockGuestPort)
	if _, err := conn.Write([]byte(connectMsg)); err != nil {
		return nil, fmt.Errorf("vsock CONNECT: %w", err)
	}

	// Read "OK <port>\n" confirmation
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("vsock CONNECT response: %w", err)
	}
	resp := string(buf[:n])
	if len(resp) < 2 || resp[:2] != "OK" {
		return nil, fmt.Errorf("vsock CONNECT rejected: %s", resp)
	}

	// Set deadline from context
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	// Send the command as JSON followed by newline delimiter
	cmd := guestCommand{
		Command: req.Command,
		Args:    req.Args,
		Stdin:   req.Stdin,
		Env:     req.Env,
		WorkDir: req.WorkDir,
		Timeout: req.TimeoutSec,
	}
	cmdData, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("marshaling command: %w", err)
	}
	cmdData = append(cmdData, '\n')
	if _, err := conn.Write(cmdData); err != nil {
		return nil, fmt.Errorf("sending command: %w", err)
	}

	// Read the response (newline-delimited JSON)
	var resultBuf bytes.Buffer
	readBuf := make([]byte, 4096)
	for {
		n, err := conn.Read(readBuf)
		if n > 0 {
			resultBuf.Write(readBuf[:n])
			// Check if we have a complete JSON response (ends with newline)
			if bytes.Contains(resultBuf.Bytes(), []byte("\n")) {
				break
			}
		}
		if err != nil {
			if resultBuf.Len() > 0 {
				break
			}
			return nil, fmt.Errorf("reading result: %w", err)
		}
	}

	var gr guestResult
	if err := json.Unmarshal(bytes.TrimSpace(resultBuf.Bytes()), &gr); err != nil {
		return nil, fmt.Errorf("unmarshaling result: %w (raw: %s)", err, resultBuf.String())
	}

	return &ExecResult{
		Stdout:   gr.Stdout,
		Stderr:   gr.Stderr,
		ExitCode: gr.ExitCode,
		Error:    gr.Error,
	}, nil
}

// executeFallback runs with the best available isolation when Firecracker is unavailable.
// On macOS, it uses sandbox-exec (Seatbelt) for process-level sandboxing.
// On other platforms, it falls back to bare process execution with a warning.
func (s *FirecrackerSandbox) executeFallback(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
	// Try macOS sandbox first
	darwin, err := NewDarwinSandbox()
	if err == nil && darwin.Available() {
		slog.Info("Firecracker unavailable, using macOS sandbox-exec fallback",
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

func (s *FirecrackerSandbox) Close() error {
	return nil
}

// fcAPICall makes a Firecracker API call via Unix socket.
func fcAPICall(client *http.Client, method, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(method, "http://localhost"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	return nil
}

// waitForSocket waits for a Unix socket to become available.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", path)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("socket %s not available after %v", path, timeout)
}

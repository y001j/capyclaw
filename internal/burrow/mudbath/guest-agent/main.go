//go:build linux

// guest-agent runs inside Firecracker microVMs and executes commands
// received over virtio-vsock. It listens on a well-known port and
// communicates via newline-delimited JSON.
//
// Build: GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o guest-agent .
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"time"
)

const listenPort = 10789

type command struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Stdin   string            `json:"stdin,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	WorkDir string            `json:"work_dir,omitempty"`
	Timeout int               `json:"timeout"`
}

type result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

func main() {
	// Listen on vsock. Under Linux, AF_VSOCK is available via /dev/vsock
	// or the MDSPROXY_CID approach. For simplicity, we listen on a TCP-like
	// vsock listener using the virtio-vsock kernel module.
	//
	// The guest kernel must have CONFIG_VIRTIO_VSOCK=y.
	// We use the Go net package with "vsock" network type if available,
	// otherwise fall back to a raw syscall listener.
	listener, err := listenVsock(listenPort)
	if err != nil {
		log.Fatalf("failed to listen on vsock port %d: %v", listenPort, err)
	}
	defer listener.Close()

	log.Printf("guest-agent listening on vsock port %d", listenPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max command

	if !scanner.Scan() {
		return
	}
	line := scanner.Bytes()

	var cmd command
	if err := json.Unmarshal(line, &cmd); err != nil {
		writeResult(conn, result{Error: fmt.Sprintf("invalid command JSON: %v", err), ExitCode: -1})
		return
	}

	res := executeCommand(cmd)
	writeResult(conn, res)
}

func executeCommand(cmd command) result {
	timeout := time.Duration(cmd.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	c := exec.Command(cmd.Command, cmd.Args...)

	if cmd.Stdin != "" {
		c.Stdin = bytes.NewBufferString(cmd.Stdin)
	}

	if cmd.WorkDir != "" {
		c.Dir = cmd.WorkDir
	}

	// Build environment: inherit minimal env + user-specified vars
	c.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/root",
	}
	for k, v := range cmd.Env {
		c.Env = append(c.Env, k+"="+v)
	}

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	// Run with timeout
	done := make(chan error, 1)
	go func() {
		done <- c.Run()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-done:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return result{
					Stdout:   stdout.String(),
					Stderr:   stderr.String(),
					ExitCode: exitErr.ExitCode(),
				}
			}
			return result{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				Error:    err.Error(),
				ExitCode: -1,
			}
		}
		return result{
			Stdout: stdout.String(),
			Stderr: stderr.String(),
		}
	case <-timer.C:
		if c.Process != nil {
			c.Process.Kill()
		}
		return result{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			Error:    "command timed out",
			ExitCode: -1,
		}
	}
}

func writeResult(conn net.Conn, res result) {
	data, _ := json.Marshal(res)
	data = append(data, '\n')
	conn.Write(data)
}

// listenVsock creates a vsock listener using raw syscalls.
// This works on Linux guests with virtio-vsock kernel support.
func listenVsock(port int) (net.Listener, error) {
	return newVsockListener(port)
}

// Platform-specific vsock listener implementation.
// Only works on Linux. See vsock_linux.go for the implementation.
// On other platforms, this file won't compile (which is fine since
// the guest agent only runs inside Linux VMs).

// For build purposes, we keep this as a stub that imports the OS-specific file.
func init() {
	// Ensure the agent doesn't accidentally run outside a VM
	if _, err := os.Stat("/sys/class/misc/vsock"); err != nil {
		// Not fatal during init; the listen call will fail with a clear error
		log.Printf("warning: vsock not detected, agent may not function correctly")
	}
}

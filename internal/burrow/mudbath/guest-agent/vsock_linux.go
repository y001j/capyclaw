//go:build linux

package main

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"
)

// AF_VSOCK and VMADDR constants for virtio-vsock.
const (
	afVsock     = 40 // AF_VSOCK
	vmaddrCIDAny = 0xFFFFFFFF // VMADDR_CID_ANY
)

// sockaddrVM is the Go representation of struct sockaddr_vm.
type sockaddrVM struct {
	Family    uint16
	Reserved1 uint16
	Port      uint32
	CID       uint32
	_         [4]byte // padding
}

func newVsockListener(port int) (net.Listener, error) {
	fd, err := syscall.Socket(afVsock, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("socket(AF_VSOCK): %w", err)
	}

	addr := sockaddrVM{
		Family: afVsock,
		Port:   uint32(port),
		CID:    vmaddrCIDAny,
	}

	_, _, errno := syscall.Syscall(
		syscall.SYS_BIND,
		uintptr(fd),
		uintptr(unsafe.Pointer(&addr)),
		unsafe.Sizeof(addr),
	)
	if errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("bind vsock port %d: %w", port, errno)
	}

	if err := syscall.Listen(fd, 5); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("listen vsock: %w", err)
	}

	file := os.NewFile(uintptr(fd), fmt.Sprintf("vsock:%d", port))
	ln, err := net.FileListener(file)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("file listener: %w", err)
	}

	return &vsockNetListener{Listener: ln, file: file}, nil
}

type vsockNetListener struct {
	net.Listener
	file *os.File
}

func (l *vsockNetListener) Close() error {
	l.file.Close()
	return l.Listener.Close()
}

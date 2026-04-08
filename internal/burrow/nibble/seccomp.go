package nibble

// SeccompProfile defines a seccomp-BPF profile for sandboxed binary execution.
// Replaces OpenClaw's string-matching safeBins allowlist.
type SeccompProfile struct {
	Name           string   `json:"name"`
	AllowedSyscalls []string `json:"allowed_syscalls"`
	ReadOnlyPaths  []string `json:"read_only_paths"`
	WritablePaths  []string `json:"writable_paths"`
}

// DefaultProfiles returns seccomp profiles for commonly used binaries.
func DefaultProfiles() map[string]*SeccompProfile {
	return map[string]*SeccompProfile{
		"python3": {
			Name: "python3",
			AllowedSyscalls: []string{
				"read", "write", "open", "close", "stat", "fstat",
				"mmap", "mprotect", "munmap", "brk", "access",
				"getpid", "clone", "execve", "wait4", "exit_group",
			},
			ReadOnlyPaths: []string{"/usr", "/lib", "/etc/alternatives"},
			WritablePaths: []string{"/tmp"},
		},
		"node": {
			Name: "node",
			AllowedSyscalls: []string{
				"read", "write", "open", "close", "stat", "fstat",
				"mmap", "mprotect", "munmap", "brk", "epoll_create1",
				"epoll_ctl", "epoll_wait", "eventfd2", "getpid",
				"clone", "futex", "exit_group",
			},
			ReadOnlyPaths: []string{"/usr", "/lib"},
			WritablePaths: []string{"/tmp"},
		},
	}
}

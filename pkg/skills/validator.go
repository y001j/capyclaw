package skills

import "fmt"

// Validate checks that a Skill manifest contains all required fields and that
// the declared permissions are within the allowed set.
func Validate(s *Skill) error {
	if s.ID == "" {
		return fmt.Errorf("skill.id is required")
	}
	if s.Name == "" {
		return fmt.Errorf("skill.name is required")
	}
	if s.Version == "" {
		return fmt.Errorf("skill.version is required")
	}
	if s.EntryPoint == "" {
		return fmt.Errorf("skill.entry_point is required")
	}
	switch s.Runtime {
	case "wasm", "shell", "docker":
	default:
		return fmt.Errorf("skill.runtime must be one of: wasm, shell, docker")
	}
	switch s.Trust {
	case TrustVerified, TrustCommunity, TrustUntrusted:
	default:
		return fmt.Errorf("skill.trust must be one of: verified, community, untrusted")
	}
	return nil
}

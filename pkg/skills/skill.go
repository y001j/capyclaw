// Package skills defines the Skill type and YAML frontmatter parser for
// CapyHub-compatible skill files.
package skills

import "time"

// TrustLevel categorises how much sandbox isolation a skill requires.
type TrustLevel string

const (
	TrustVerified   TrustLevel = "verified"   // CapyHub-signed, runs in Wazero (Tier 1)
	TrustCommunity  TrustLevel = "community"  // User-installed, runs in gVisor (Tier 2)
	TrustUntrusted  TrustLevel = "untrusted"  // Unknown origin, runs in Firecracker (Tier 3)
)

// Skill represents a parsed skill manifest.
type Skill struct {
	// Manifest fields (from YAML frontmatter)
	ID          string            `yaml:"id"          json:"id"`
	Name        string            `yaml:"name"        json:"name"`
	Version     string            `yaml:"version"     json:"version"`
	Description string            `yaml:"description" json:"description"`
	Author      string            `yaml:"author"      json:"author"`
	Trust       TrustLevel        `yaml:"trust"       json:"trust"`
	Permissions []string          `yaml:"permissions" json:"permissions"`
	Tags        []string          `yaml:"tags"        json:"tags"`
	EntryPoint  string            `yaml:"entry_point" json:"entry_point"`
	Runtime     string            `yaml:"runtime"     json:"runtime"` // "wasm" | "shell" | "docker"
	Metadata    map[string]string `yaml:"metadata"    json:"metadata,omitempty"`

	// Resolved at load time
	SourcePath  string    `yaml:"-" json:"-"`
	InstalledAt time.Time `yaml:"-" json:"installed_at,omitempty"`
}

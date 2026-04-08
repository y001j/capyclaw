package skills

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Loader discovers and loads skill manifests from a directory tree.
type Loader struct {
	rootDir string
}

// NewLoader creates a Loader rooted at dir.
func NewLoader(dir string) *Loader {
	return &Loader{rootDir: dir}
}

// Load returns all valid skills found under the root directory.
// Each skill directory must contain a skill.yaml manifest file.
func (l *Loader) Load() ([]*Skill, error) {
	entries, err := os.ReadDir(l.rootDir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}

	var skills []*Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifestPath := filepath.Join(l.rootDir, e.Name(), "skill.yaml")
		s, err := parseManifest(manifestPath)
		if err != nil {
			slog.Warn("skipping invalid skill", "path", manifestPath, "error", err)
			continue
		}
		skills = append(skills, s)
	}
	return skills, nil
}

// parseManifest reads and parses a skill YAML manifest file.
// Supports two formats:
// 1. YAML frontmatter: --- delimited YAML followed by markdown content
// 2. Plain YAML: the entire file is YAML
func parseManifest(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var skill Skill

	// Check for YAML frontmatter (--- delimited)
	if bytes.HasPrefix(data, []byte("---\n")) || bytes.HasPrefix(data, []byte("---\r\n")) {
		parts := bytes.SplitN(data, []byte("---\n"), 3)
		if len(parts) >= 3 {
			// Frontmatter format: ---\n<yaml>\n---\n<content>
			if err := yaml.Unmarshal(parts[1], &skill); err != nil {
				return nil, fmt.Errorf("parsing skill frontmatter: %w", err)
			}
		} else {
			// Try splitting with just two ---
			parts = bytes.SplitN(data[4:], []byte("\n---"), 2)
			if len(parts) >= 1 {
				if err := yaml.Unmarshal(parts[0], &skill); err != nil {
					return nil, fmt.Errorf("parsing skill frontmatter: %w", err)
				}
			}
		}
	} else {
		// Plain YAML format
		if err := yaml.Unmarshal(data, &skill); err != nil {
			return nil, fmt.Errorf("parsing skill manifest: %w", err)
		}
	}

	skill.SourcePath = filepath.Dir(path)

	if err := Validate(&skill); err != nil {
		return nil, err
	}

	return &skill, nil
}

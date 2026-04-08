package canopy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SOPSClient provides SOPS-based secret management as a fallback for dev environments.
type SOPSClient struct {
	filePath string
}

// NewSOPSClient creates a new SOPS client.
func NewSOPSClient(filePath string) *SOPSClient {
	return &SOPSClient{filePath: filePath}
}

// GetSecret retrieves a secret from a SOPS-encrypted file.
func (s *SOPSClient) GetSecret(ctx context.Context, key string) (string, error) {
	// Decrypt the file using the sops CLI
	cmd := exec.CommandContext(ctx, "sops", "--decrypt", s.filePath)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("decrypting SOPS file %s: %w", s.filePath, err)
	}

	// Parse the decrypted content as JSON or YAML
	var data map[string]any
	if err := json.Unmarshal(output, &data); err != nil {
		return "", fmt.Errorf("parsing decrypted SOPS content: %w", err)
	}

	// Support dot-separated key paths (e.g., "llm.anthropic.api_key")
	val, ok := getNestedKey(data, key)
	if !ok {
		return "", fmt.Errorf("key %q not found in SOPS file", key)
	}

	return fmt.Sprintf("%v", val), nil
}

// getNestedKey retrieves a value from a nested map using a dot-separated key path.
func getNestedKey(data map[string]any, key string) (any, bool) {
	parts := strings.Split(key, ".")
	current := any(data)

	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}

	return current, true
}

// FromEnv retrieves a secret from an environment variable (simplest fallback).
func FromEnv(key string) string {
	return os.Getenv(key)
}

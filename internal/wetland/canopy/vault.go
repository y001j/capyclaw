package canopy

import (
	"context"
	"fmt"
	"strings"

	vaultapi "github.com/hashicorp/vault/api"
)

// VaultClient manages secrets via HashiCorp Vault.
type VaultClient struct {
	client    *vaultapi.Client
	mountPath string
}

// NewVaultClient creates a new Vault client.
func NewVaultClient(address, mountPath string) (*VaultClient, error) {
	config := vaultapi.DefaultConfig()
	config.Address = address

	client, err := vaultapi.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("creating Vault client: %w", err)
	}

	return &VaultClient{
		client:    client,
		mountPath: mountPath,
	}, nil
}

// GetSecret retrieves a secret from Vault.
func (v *VaultClient) GetSecret(ctx context.Context, path string) (map[string]any, error) {
	secret, err := v.client.KVv2(v.mountPath).Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("reading secret %s: %w", path, err)
	}
	return secret.Data, nil
}

// ResolveRef resolves a vault:// reference to its actual value.
func (v *VaultClient) ResolveRef(ctx context.Context, ref string) (string, error) {
	if !strings.HasPrefix(ref, "vault://") {
		return ref, nil // not a Vault reference
	}

	path := strings.TrimPrefix(ref, "vault://")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid Vault reference: %s", ref)
	}

	data, err := v.GetSecret(ctx, parts[1])
	if err != nil {
		return "", err
	}

	val, ok := data["value"].(string)
	if !ok {
		return "", fmt.Errorf("secret %s has no 'value' field", path)
	}

	return val, nil
}

package canopy

import (
	"context"
	"log/slog"
	"time"
)

// RotateCallback is called when a secret needs rotation.
type RotateCallback func(path string)

// SecretRotator automatically rotates secrets on a schedule.
type SecretRotator struct {
	vault          *VaultClient
	interval       time.Duration
	monitoredPaths []string
	onRotate       RotateCallback
}

// NewSecretRotator creates a new secret rotator.
func NewSecretRotator(vault *VaultClient, interval time.Duration) *SecretRotator {
	return &SecretRotator{
		vault:    vault,
		interval: interval,
		monitoredPaths: []string{
			"capyclaw/llm/anthropic",
			"capyclaw/llm/openai",
			"capyclaw/jwt/signing-key",
		},
	}
}

// SetOnRotate sets the callback for when a secret is rotated.
func (r *SecretRotator) SetOnRotate(cb RotateCallback) {
	r.onRotate = cb
}

// SetMonitoredPaths sets the Vault paths to monitor for rotation.
func (r *SecretRotator) SetMonitoredPaths(paths []string) {
	r.monitoredPaths = paths
}

// Start begins the rotation loop.
func (r *SecretRotator) Start(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.rotate(ctx)
		}
	}
}

func (r *SecretRotator) rotate(ctx context.Context) {
	slog.Info("checking for secret rotation")

	for _, path := range r.monitoredPaths {
		// Read the secret metadata to check age
		metadata, err := r.vault.client.KVv2(r.vault.mountPath).GetMetadata(ctx, path)
		if err != nil {
			slog.Debug("skipping secret rotation check", "path", path, "error", err)
			continue
		}

		// Check if the secret is older than the rotation interval
		if time.Since(metadata.CreatedTime) < r.interval {
			continue
		}

		slog.Info("secret rotation needed",
			"path", path,
			"age", time.Since(metadata.CreatedTime).String(),
			"interval", r.interval.String(),
		)

		// Notify dependent services via callback
		if r.onRotate != nil {
			r.onRotate(path)
		}
	}
}

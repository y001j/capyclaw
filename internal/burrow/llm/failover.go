package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/sony/gobreaker/v2"
)

// FailoverClient wraps multiple LLM providers with circuit breaker support.
// It routes requests to the correct provider based on model name,
// falling back to sequential failover if no model mapping is found.
type FailoverClient struct {
	mu           sync.RWMutex
	providers    []providerEntry
	modelToIndex map[string]int // model name → provider index for direct routing
}

type providerEntry struct {
	client  Client
	breaker *gobreaker.CircuitBreaker[*Chunk]
}

// ProviderWithModels pairs a client with its supported model list.
type ProviderWithModels struct {
	Client Client
	Models []string
}

// NewFailoverClient creates a client that automatically fails over between providers.
func NewFailoverClient(clients ...Client) *FailoverClient {
	fc := &FailoverClient{modelToIndex: make(map[string]int)}

	for _, c := range clients {
		cb := gobreaker.NewCircuitBreaker[*Chunk](gobreaker.Settings{
			Name: c.Provider(),
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures > 3
			},
		})
		fc.providers = append(fc.providers, providerEntry{
			client:  c,
			breaker: cb,
		})
	}

	return fc
}

// NewRoutedFailoverClient creates a client that routes by model name
// and falls back to sequential failover for unknown models.
func NewRoutedFailoverClient(pwms ...ProviderWithModels) *FailoverClient {
	fc := &FailoverClient{modelToIndex: make(map[string]int)}

	for _, pwm := range pwms {
		cb := gobreaker.NewCircuitBreaker[*Chunk](gobreaker.Settings{
			Name: pwm.Client.Provider(),
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures > 3
			},
		})
		idx := len(fc.providers)
		fc.providers = append(fc.providers, providerEntry{
			client:  pwm.Client,
			breaker: cb,
		})
		for _, model := range pwm.Models {
			fc.modelToIndex[model] = idx
		}
	}

	return fc
}

// Stream routes the request to the matching provider by model name,
// then falls back to sequential failover if direct routing fails.
func (fc *FailoverClient) Stream(ctx context.Context, req *Request) (<-chan *Chunk, error) {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	// Try direct model routing first
	if idx, ok := fc.modelToIndex[req.Model]; ok {
		p := fc.providers[idx]
		if p.breaker.State() != gobreaker.StateOpen {
			ch, err := p.client.Stream(ctx, req)
			if err == nil {
				return ch, nil
			}
			slog.Warn("routed provider failed, falling back",
				"provider", p.client.Provider(),
				"model", req.Model,
				"error", err,
			)
		}
	}

	// Sequential failover
	var lastErr error
	for _, p := range fc.providers {
		if p.breaker.State() == gobreaker.StateOpen {
			slog.Debug("skipping provider (circuit open)", "provider", p.client.Provider())
			continue
		}

		ch, err := p.client.Stream(ctx, req)
		if err != nil {
			lastErr = err
			slog.Warn("provider failed, trying next",
				"provider", p.client.Provider(),
				"error", err,
			)
			continue
		}

		return ch, nil
	}

	return nil, fmt.Errorf("all providers failed: %w", lastErr)
}

// CountTokens delegates to the first available provider.
func (fc *FailoverClient) CountTokens(ctx context.Context, messages []Message) (int, error) {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	if len(fc.providers) > 0 {
		return fc.providers[0].client.CountTokens(ctx, messages)
	}
	return 0, fmt.Errorf("no providers available")
}

// Provider returns "failover".
func (fc *FailoverClient) Provider() string {
	return "failover"
}

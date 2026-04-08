package adapters

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Manager manages the lifecycle of all channel adapters and routes
// incoming messages to the appropriate agent pipeline.
type Manager struct {
	mu       sync.RWMutex
	adapters map[string]ChannelAdapter
	handler  MessageHandler
}

// NewManager creates a new adapter manager.
func NewManager(handler MessageHandler) *Manager {
	return &Manager{
		adapters: make(map[string]ChannelAdapter),
		handler:  handler,
	}
}

// Register adds an adapter to the manager.
func (m *Manager) Register(adapter ChannelAdapter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adapters[adapter.Name()] = adapter
}

// StartAll starts all registered adapters.
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, adapter := range m.adapters {
		slog.Info("starting channel adapter", "adapter", name)
		go func(name string, a ChannelAdapter) {
			if err := a.Start(ctx); err != nil {
				slog.Error("channel adapter failed",
					"adapter", name,
					"error", err,
				)
			}
		}(name, adapter)
	}

	return nil
}

// StopAll gracefully stops all adapters.
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errs []error
	for name, adapter := range m.adapters {
		slog.Info("stopping channel adapter", "adapter", name)
		if err := adapter.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stopping %s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("stopping adapters: %v", errs)
	}
	return nil
}

// Get returns an adapter by name.
func (m *Manager) Get(name string) (ChannelAdapter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.adapters[name]
	return a, ok
}

// SendMessage sends a message through the specified adapter.
func (m *Manager) SendMessage(ctx context.Context, adapterName, target string, msg *OutgoingMessage) error {
	m.mu.RLock()
	adapter, ok := m.adapters[adapterName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("adapter %s not found", adapterName)
	}

	return adapter.SendMessage(ctx, target, msg)
}

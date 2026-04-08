package vapor

import (
	"context"
	"sync"
)

// HealthChecker aggregates health status from all subsystems.
type HealthChecker struct {
	mu     sync.RWMutex
	checks map[string]HealthCheckFunc
}

// HealthCheckFunc performs a health check and returns nil if healthy.
type HealthCheckFunc func(ctx context.Context) error

// HealthStatus contains the overall system health.
type HealthStatus struct {
	Status     string            `json:"status"` // healthy, degraded, unhealthy
	Components map[string]string `json:"components"`
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		checks: make(map[string]HealthCheckFunc),
	}
}

// Register adds a health check for a component.
func (h *HealthChecker) Register(name string, check HealthCheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
}

// Check runs all health checks and returns the aggregate status.
func (h *HealthChecker) Check(ctx context.Context) *HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := &HealthStatus{
		Status:     "healthy",
		Components: make(map[string]string),
	}

	for name, check := range h.checks {
		if err := check(ctx); err != nil {
			status.Components[name] = "unhealthy: " + err.Error()
			status.Status = "degraded"
		} else {
			status.Components[name] = "healthy"
		}
	}

	return status
}

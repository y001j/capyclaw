package footprint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Exporter exports audit events to external SIEM systems via HTTP.
type Exporter struct {
	logger   *Logger
	endpoint string
	enabled  bool
	interval time.Duration
	lastID   int64
	client   *http.Client
}

// NewExporter creates a new audit event exporter.
func NewExporter(logger *Logger, endpoint string, enabled bool, interval time.Duration) *Exporter {
	return &Exporter{
		logger:   logger,
		endpoint: endpoint,
		enabled:  enabled,
		interval: interval,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Start begins the periodic export loop.
func (e *Exporter) Start(ctx context.Context) {
	if !e.enabled {
		slog.Info("audit exporter disabled")
		return
	}

	slog.Info("audit exporter started", "endpoint", e.endpoint, "interval", e.interval)
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.export(ctx); err != nil {
				slog.Error("audit export failed", "error", err)
			}
		}
	}
}

func (e *Exporter) export(ctx context.Context) error {
	// Query events newer than the last exported ID
	filter := AuditFilter{
		Limit: 200,
	}
	events, err := e.logger.Query(ctx, filter)
	if err != nil {
		return fmt.Errorf("querying events for export: %w", err)
	}

	if len(events) == 0 {
		return nil
	}

	// Filter to only events after lastID
	var toExport []*AuditEvent
	for _, ev := range events {
		if ev.ID > e.lastID {
			toExport = append(toExport, ev)
		}
	}
	if len(toExport) == 0 {
		return nil
	}

	// Batch POST to SIEM endpoint
	body, err := json.Marshal(toExport)
	if err != nil {
		return fmt.Errorf("marshaling events: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating export request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending events to SIEM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("SIEM returned status %d", resp.StatusCode)
	}

	// Update cursor to the highest exported ID
	for _, ev := range toExport {
		if ev.ID > e.lastID {
			e.lastID = ev.ID
		}
	}

	slog.Debug("exported audit events", "count", len(toExport), "last_id", e.lastID)
	return nil
}

// Export sends the given audit events to the configured SIEM endpoint.
func (e *Exporter) Export(ctx context.Context, events []*AuditEvent) error {
	if !e.enabled || len(events) == 0 {
		return nil
	}

	body, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("marshaling events: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating export request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending events to SIEM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("SIEM returned status %d", resp.StatusCode)
	}

	slog.Debug("exported audit events", "count", len(events), "endpoint", e.endpoint)
	return nil
}

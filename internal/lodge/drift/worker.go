package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"CapyClaw/internal/pond/ripple"
)

// PipelineExecutor is the interface for executing agent pipeline tasks.
// This avoids circular imports with the burrow package.
type PipelineExecutor interface {
	ExecutePrompt(ctx context.Context, agentID, tenantID, prompt, sessionKey string) (string, error)
}

// Worker processes async tasks via Asynq.
type Worker struct {
	server    *asynq.Server
	embedding ripple.EmbeddingGenerator
	pool      *pgxpool.Pool
	pipeline  PipelineExecutor
}

// NewWorker creates a new async task worker.
func NewWorker(redisAddr string, embedding ripple.EmbeddingGenerator, pool *pgxpool.Pool) *Worker {
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency: 10,
			Queues: map[string]int{
				"default":  6,
				"critical": 3,
				"low":      1,
			},
		},
	)
	return &Worker{
		server:    srv,
		embedding: embedding,
		pool:      pool,
	}
}

// SetPipeline sets the pipeline executor for cron tasks.
func (w *Worker) SetPipeline(p PipelineExecutor) {
	w.pipeline = p
}

// Start begins processing async tasks.
func (w *Worker) Start() error {
	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeEmbeddingGenerate, w.handleEmbeddingGenerate)
	mux.HandleFunc(TypeMemoryCompact, w.handleMemoryCompact)
	mux.HandleFunc(TypeCronExecute, w.handleCronExecute)
	return w.server.Start(mux)
}

// Stop gracefully shuts down the worker.
func (w *Worker) Stop() {
	w.server.Stop()
}

// handleEmbeddingGenerate processes an embedding generation task.
func (w *Worker) handleEmbeddingGenerate(ctx context.Context, task *asynq.Task) error {
	var payload struct {
		MemoryID string `json:"memory_id"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshalling payload: %w", err)
	}

	slog.Info("generating embedding",
		"memory_id", payload.MemoryID,
		"content_length", len(payload.Content),
	)

	// Generate embedding vector
	vector, err := w.embedding.Generate(ctx, payload.Content)
	if err != nil {
		return fmt.Errorf("generating embedding: %w", err)
	}

	// Update the memory record with the embedding
	// pgvector expects the vector as a string like '[0.1, 0.2, ...]'
	vectorStr := vectorToString(vector)
	_, err = w.pool.Exec(ctx,
		"UPDATE memories SET embedding = $1::vector WHERE id = $2",
		vectorStr, payload.MemoryID,
	)
	if err != nil {
		return fmt.Errorf("updating embedding: %w", err)
	}

	slog.Info("embedding generated successfully",
		"memory_id", payload.MemoryID,
		"dimensions", len(vector),
	)
	return nil
}

// handleMemoryCompact processes a memory compaction task.
func (w *Worker) handleMemoryCompact(ctx context.Context, task *asynq.Task) error {
	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshalling payload: %w", err)
	}

	slog.Info("processing memory compaction", "session_id", payload.SessionID)
	// Memory compaction is handled by the Compactor in the pipeline.
	// This task can be used for scheduled background compaction.
	return nil
}

// handleCronExecute processes a cron-triggered agent execution task.
func (w *Worker) handleCronExecute(ctx context.Context, task *asynq.Task) error {
	var payload CronExecutePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshalling cron payload: %w", err)
	}

	slog.Info("executing cron job",
		"cron_job_id", payload.CronJobID,
		"agent_id", payload.AgentID,
		"job_name", payload.JobName,
	)

	if w.pipeline == nil {
		return fmt.Errorf("pipeline not configured for cron execution")
	}

	// Reuse the same session for each cron job so history accumulates
	sessionKey := fmt.Sprintf("cron:%s", payload.CronJobID)

	// Execute through the pipeline
	_, err := w.pipeline.ExecutePrompt(ctx, payload.AgentID, payload.TenantID, payload.Prompt, sessionKey)
	if err != nil {
		slog.Error("cron execution failed",
			"cron_job_id", payload.CronJobID,
			"error", err,
		)
		// Update last_run_at even on failure
		w.updateCronJobRun(ctx, payload.CronJobID, err)
		return fmt.Errorf("cron execution: %w", err)
	}

	// Update run stats
	w.updateCronJobRun(ctx, payload.CronJobID, nil)

	slog.Info("cron job completed",
		"cron_job_id", payload.CronJobID,
		"job_name", payload.JobName,
	)
	return nil
}

// updateCronJobRun updates the cron job's run statistics in the database.
func (w *Worker) updateCronJobRun(ctx context.Context, cronJobID string, execErr error) {
	status := "success"
	if execErr != nil {
		status = "failed"
	}

	_, err := w.pool.Exec(ctx,
		`UPDATE cron_jobs
		 SET last_run_at = NOW(),
		     run_count = run_count + 1,
		     last_status = $2
		 WHERE id = $1`,
		cronJobID, status,
	)
	if err != nil {
		slog.Error("updating cron job run stats",
			"cron_job_id", cronJobID,
			"error", err,
		)
	}
}

// vectorToString converts a float32 slice to pgvector string format.
func vectorToString(v []float32) string {
	buf := make([]byte, 0, len(v)*10+2)
	buf = append(buf, '[')
	for i, f := range v {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, []byte(fmt.Sprintf("%g", f))...)
	}
	buf = append(buf, ']')
	return string(buf)
}

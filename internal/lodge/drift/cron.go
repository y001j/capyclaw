package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CronJob represents a scheduled job from the database.
type CronJob struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	AgentID   uuid.UUID `json:"agent_id"`
	Name      string    `json:"name"`
	Schedule  string    `json:"schedule"`
	Prompt    string    `json:"prompt"`
	Enabled   bool      `json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
	RunCount  int       `json:"run_count"`
}

// CronExecutePayload is the payload for cron execution tasks.
type CronExecutePayload struct {
	CronJobID string `json:"cron_job_id"`
	AgentID   string `json:"agent_id"`
	TenantID  string `json:"tenant_id"`
	Prompt    string `json:"prompt"`
	JobName   string `json:"job_name"`
}

// CronManager manages cron job scheduling via Asynq scheduler.
type CronManager struct {
	scheduler *asynq.Scheduler
	pool      *pgxpool.Pool
	entryIDs  map[uuid.UUID]string // cron_job_id → scheduler entry_id
}

// NewCronManager creates a new cron manager.
func NewCronManager(redisAddr string, pool *pgxpool.Pool) *CronManager {
	return &CronManager{
		scheduler: asynq.NewScheduler(
			asynq.RedisClientOpt{Addr: redisAddr},
			nil,
		),
		pool:     pool,
		entryIDs: make(map[uuid.UUID]string),
	}
}

// LoadAndSchedule loads all enabled cron jobs from the database and registers them.
func (m *CronManager) LoadAndSchedule(ctx context.Context) error {
	rows, err := m.pool.Query(ctx,
		`SELECT id, tenant_id, agent_id, name, schedule, prompt, enabled
		 FROM cron_jobs WHERE enabled = true`,
	)
	if err != nil {
		return fmt.Errorf("querying cron jobs: %w", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var job CronJob
		if err := rows.Scan(&job.ID, &job.TenantID, &job.AgentID, &job.Name, &job.Schedule, &job.Prompt, &job.Enabled); err != nil {
			slog.Error("scanning cron job", "error", err)
			continue
		}

		if err := m.ScheduleJob(job); err != nil {
			slog.Error("scheduling cron job",
				"job_id", job.ID,
				"name", job.Name,
				"error", err,
			)
			continue
		}
		count++
	}

	slog.Info("loaded cron jobs", "count", count)
	return nil
}

// ScheduleJob registers a single cron job with the scheduler.
func (m *CronManager) ScheduleJob(job CronJob) error {
	payload, err := json.Marshal(CronExecutePayload{
		CronJobID: job.ID.String(),
		AgentID:   job.AgentID.String(),
		TenantID:  job.TenantID.String(),
		Prompt:    job.Prompt,
		JobName:   job.Name,
	})
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	task := asynq.NewTask(TypeCronExecute, payload)
	entryID, err := m.scheduler.Register(job.Schedule, task,
		asynq.Queue("default"),
	)
	if err != nil {
		return fmt.Errorf("registering cron %q (%s): %w", job.Name, job.Schedule, err)
	}

	m.entryIDs[job.ID] = entryID
	slog.Info("registered cron job",
		"job_id", job.ID,
		"name", job.Name,
		"schedule", job.Schedule,
		"entry_id", entryID,
	)
	return nil
}

// UnscheduleJob removes a cron job from the scheduler.
func (m *CronManager) UnscheduleJob(jobID uuid.UUID) error {
	entryID, ok := m.entryIDs[jobID]
	if !ok {
		return fmt.Errorf("cron job %s not scheduled", jobID)
	}
	if err := m.scheduler.Unregister(entryID); err != nil {
		return fmt.Errorf("unregistering cron %s: %w", jobID, err)
	}
	delete(m.entryIDs, jobID)
	return nil
}

// Register registers a cron job directly (for backward compatibility).
func (m *CronManager) Register(cronExpr, taskType string, payload []byte) (string, error) {
	task := asynq.NewTask(taskType, payload)
	entryID, err := m.scheduler.Register(cronExpr, task)
	if err != nil {
		return "", err
	}
	slog.Info("registered cron job", "entry_id", entryID, "cron", cronExpr, "task", taskType)
	return entryID, nil
}

// Start starts the cron scheduler.
func (m *CronManager) Start() error {
	return m.scheduler.Start()
}

// Shutdown stops the cron scheduler.
func (m *CronManager) Shutdown() {
	m.scheduler.Shutdown()
}

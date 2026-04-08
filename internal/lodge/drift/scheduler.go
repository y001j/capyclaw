package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"
)

// Task type names.
const (
	TypeEmbeddingGenerate = "embedding:generate"
	TypeSessionExport     = "session:export"
	TypeMemoryCompact     = "memory:compact"
	TypeCronExecute       = "cron:execute"
)

// Scheduler manages async task dispatch via Asynq.
type Scheduler struct {
	client *asynq.Client
}

// NewScheduler creates a new task scheduler.
func NewScheduler(redisAddr string) *Scheduler {
	return &Scheduler{
		client: asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr}),
	}
}

// EnqueueEmbedding enqueues an embedding generation task.
func (s *Scheduler) EnqueueEmbedding(ctx context.Context, memoryID, content string) error {
	payload, _ := json.Marshal(map[string]string{
		"memory_id": memoryID,
		"content":   content,
	})
	task := asynq.NewTask(TypeEmbeddingGenerate, payload)
	info, err := s.client.Enqueue(task)
	if err != nil {
		return fmt.Errorf("enqueue embedding task: %w", err)
	}
	slog.Debug("enqueued embedding task", "task_id", info.ID)
	return nil
}

// Close closes the Asynq client.
func (s *Scheduler) Close() error {
	return s.client.Close()
}

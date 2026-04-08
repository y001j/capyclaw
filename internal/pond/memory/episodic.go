package memory

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EpisodicStore manages episodic (conversation) memories.
// Uses vector similarity search with time-decay weighting.
type EpisodicStore struct {
	pool     *pgxpool.Pool
	embedder EmbeddingGeneratorFunc
}

// NewEpisodicStore creates a new episodic memory store.
func NewEpisodicStore(pool *pgxpool.Pool, embedder EmbeddingGeneratorFunc) *EpisodicStore {
	return &EpisodicStore{pool: pool, embedder: embedder}
}

// SearchByVector retrieves episodic memories by pre-computed embedding, weighted by importance and time decay.
func (s *EpisodicStore) SearchByVector(ctx context.Context, agentID uuid.UUID, embedding []float32, limit int) ([]MemoryEntry, error) {
	if s.pool == nil {
		return nil, nil
	}

	embStr := vectorToString(embedding)
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, tenant_id, content, importance_score, COALESCE(access_count, 0)
		FROM memories
		WHERE agent_id = $1 AND memory_type = 'episodic'
		  AND embedding IS NOT NULL
		ORDER BY importance_score
			* EXP(-0.1 * EXTRACT(EPOCH FROM (NOW() - created_at)) / 86400)
			* (1 - (embedding <=> $2::vector))
			DESC
		LIMIT $3
	`, agentID, embStr, limit)
	if err != nil {
		return nil, fmt.Errorf("episodic search: %w", err)
	}
	defer rows.Close()

	var results []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		e.MemoryType = "episodic"
		if err := rows.Scan(&e.ID, &e.AgentID, &e.TenantID, &e.Content, &e.ImportanceScore, &e.AccessCount); err != nil {
			return nil, fmt.Errorf("scanning episodic memory: %w", err)
		}
		results = append(results, e)
	}

	// Increment access count
	if len(results) > 0 {
		ids := make([]uuid.UUID, len(results))
		for i, r := range results {
			ids[i] = r.ID
		}
		_, _ = s.pool.Exec(ctx, `
			UPDATE memories
			SET access_count = COALESCE(access_count, 0) + 1, last_accessed_at = NOW()
			WHERE id = ANY($1)
		`, ids)
	}

	return results, nil
}

// Store saves a new episodic memory and generates embedding asynchronously.
func (s *EpisodicStore) Store(ctx context.Context, entry *MemoryEntry) error {
	if s.pool == nil {
		return fmt.Errorf("episodic store not initialized")
	}

	var memoryID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO memories (agent_id, tenant_id, memory_type, content, importance_score, source_session_id)
		VALUES ($1, $2, 'episodic', $3, $4, $5)
		RETURNING id
	`, entry.AgentID, entry.TenantID, entry.Content, entry.ImportanceScore, entry.SourceSessionID).Scan(&memoryID)
	if err != nil {
		return fmt.Errorf("storing episodic memory: %w", err)
	}
	entry.ID = memoryID

	// Generate embedding asynchronously
	if s.embedder != nil {
		go func() {
			embedding, err := s.embedder(context.Background(), entry.Content)
			if err != nil {
				return
			}
			embStr := vectorToString(embedding)
			_, _ = s.pool.Exec(context.Background(),
				`UPDATE memories SET embedding = $1::vector WHERE id = $2`,
				embStr, memoryID,
			)
		}()
	}

	return nil
}

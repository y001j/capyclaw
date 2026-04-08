package memory

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Consolidator handles memory compaction, deduplication, and decay.
type Consolidator struct {
	pool                *pgxpool.Pool
	maxPerAgent         int     // hard cap on memories per agent
	similarityThreshold float64 // cosine similarity threshold for merging (e.g. 0.92)
	decayThreshold      float64 // minimum decayed score to keep episodic memories (e.g. 0.05)
}

// NewConsolidator creates a memory consolidator.
func NewConsolidator(pool *pgxpool.Pool, maxPerAgent int, similarityThreshold, decayThreshold float64) *Consolidator {
	if maxPerAgent <= 0 {
		maxPerAgent = 500
	}
	if similarityThreshold <= 0 {
		similarityThreshold = 0.92
	}
	if decayThreshold <= 0 {
		decayThreshold = 0.05
	}
	return &Consolidator{
		pool:                pool,
		maxPerAgent:         maxPerAgent,
		similarityThreshold: similarityThreshold,
		decayThreshold:      decayThreshold,
	}
}

// Consolidate runs all compaction steps for a single agent.
func (c *Consolidator) Consolidate(ctx context.Context, agentID uuid.UUID) error {
	// 1. Count current memories
	var count int
	err := c.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE agent_id = $1`, agentID).Scan(&count)
	if err != nil {
		return fmt.Errorf("counting memories: %w", err)
	}

	slog.Info("consolidation starting", "agent_id", agentID, "memory_count", count)

	// 2. Boost importance for frequently accessed memories
	boosted, err := c.boostFrequentlyAccessed(ctx, agentID)
	if err != nil {
		slog.Warn("importance boost failed", "error", err)
	} else if boosted > 0 {
		slog.Info("boosted frequently accessed memories", "count", boosted, "agent_id", agentID)
	}

	// 3. Merge duplicate/similar memories (only those with embeddings)
	merged, err := c.mergeSimilar(ctx, agentID)
	if err != nil {
		slog.Warn("merge similar failed", "error", err)
	} else if merged > 0 {
		slog.Info("merged similar memories", "count", merged, "agent_id", agentID)
	}

	// 4. Prune decayed episodic memories (low importance + old + rarely accessed)
	pruned, err := c.pruneDecayedEpisodic(ctx, agentID)
	if err != nil {
		slog.Warn("episodic pruning failed", "error", err)
	} else if pruned > 0 {
		slog.Info("pruned decayed episodic memories", "count", pruned, "agent_id", agentID)
	}

	// 5. Hard cap enforcement
	capped, err := c.enforceHardCap(ctx, agentID)
	if err != nil {
		slog.Warn("hard cap enforcement failed", "error", err)
	} else if capped > 0 {
		slog.Info("hard cap removed memories", "count", capped, "agent_id", agentID)
	}

	return nil
}

// boostFrequentlyAccessed increases importance for memories accessed many times.
func (c *Consolidator) boostFrequentlyAccessed(ctx context.Context, agentID uuid.UUID) (int64, error) {
	tag, err := c.pool.Exec(ctx, `
		UPDATE memories
		SET importance_score = LEAST(importance_score + 0.05, 1.0),
		    updated_at = NOW()
		WHERE agent_id = $1
		  AND COALESCE(access_count, 0) >= 5
		  AND importance_score < 0.9
	`, agentID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// mergeSimilar finds and merges memories with high cosine similarity.
func (c *Consolidator) mergeSimilar(ctx context.Context, agentID uuid.UUID) (int, error) {
	// Find similar pairs (only among memories that have embeddings)
	rows, err := c.pool.Query(ctx, `
		SELECT m1.id, m2.id, m1.content, m2.content,
		       m1.importance_score, m2.importance_score,
		       COALESCE(m1.access_count, 0), COALESCE(m2.access_count, 0),
		       1 - (m1.embedding <=> m2.embedding) as similarity
		FROM memories m1
		JOIN memories m2 ON m1.agent_id = m2.agent_id
		  AND m1.memory_type = m2.memory_type
		  AND m1.id < m2.id
		WHERE m1.agent_id = $1
		  AND m1.embedding IS NOT NULL
		  AND m2.embedding IS NOT NULL
		  AND 1 - (m1.embedding <=> m2.embedding) > $2
		ORDER BY similarity DESC
		LIMIT 50
	`, agentID, c.similarityThreshold)
	if err != nil {
		return 0, fmt.Errorf("querying similar pairs: %w", err)
	}
	defer rows.Close()

	type pair struct {
		id1, id2                     uuid.UUID
		content1, content2           string
		importance1, importance2     float64
		accessCount1, accessCount2   int
	}

	var pairs []pair
	for rows.Next() {
		var p pair
		var sim float64
		if err := rows.Scan(&p.id1, &p.id2, &p.content1, &p.content2,
			&p.importance1, &p.importance2, &p.accessCount1, &p.accessCount2, &sim); err != nil {
			return 0, fmt.Errorf("scanning pair: %w", err)
		}
		pairs = append(pairs, p)
	}

	// Track deleted IDs to avoid cascading merges
	deleted := make(map[uuid.UUID]bool)
	merged := 0

	for _, p := range pairs {
		if deleted[p.id1] || deleted[p.id2] {
			continue
		}

		// Keep the one with higher score (importance * (access_count + 1))
		score1 := p.importance1 * float64(p.accessCount1+1)
		score2 := p.importance2 * float64(p.accessCount2+1)

		keepID, deleteID := p.id1, p.id2
		keepImportance := p.importance1
		totalAccess := p.accessCount1 + p.accessCount2
		if score2 > score1 {
			keepID, deleteID = p.id2, p.id1
			keepImportance = p.importance2
		}

		// Boost the keeper's importance (merged memory is more important)
		newImportance := keepImportance + 0.05
		if newImportance > 1.0 {
			newImportance = 1.0
		}

		// Update keeper with combined access count and boosted importance
		_, err := c.pool.Exec(ctx, `
			UPDATE memories
			SET access_count = $1, importance_score = $2, updated_at = NOW()
			WHERE id = $3
		`, totalAccess, newImportance, keepID)
		if err != nil {
			slog.Warn("failed to update merged memory", "error", err)
			continue
		}

		// Delete the duplicate
		_, err = c.pool.Exec(ctx, `DELETE FROM memories WHERE id = $1`, deleteID)
		if err != nil {
			slog.Warn("failed to delete duplicate memory", "error", err)
			continue
		}

		deleted[deleteID] = true
		merged++
	}

	return merged, nil
}

// pruneDecayedEpisodic removes old, low-importance, rarely-accessed episodic memories.
func (c *Consolidator) pruneDecayedEpisodic(ctx context.Context, agentID uuid.UUID) (int64, error) {
	// Formula: importance * exp(-0.1 * days_old) < threshold AND access_count < 2
	// This preserves:
	//   - High importance memories (user emphasized)
	//   - Frequently accessed memories (repeatedly useful)
	//   - Recent memories (not yet decayed)
	tag, err := c.pool.Exec(ctx, `
		DELETE FROM memories
		WHERE agent_id = $1
		  AND memory_type = 'episodic'
		  AND importance_score * EXP(-0.1 * EXTRACT(EPOCH FROM (NOW() - created_at)) / 86400) < $2
		  AND COALESCE(access_count, 0) < 2
	`, agentID, c.decayThreshold)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// enforceHardCap removes lowest-scoring memories when count exceeds the limit.
func (c *Consolidator) enforceHardCap(ctx context.Context, agentID uuid.UUID) (int64, error) {
	var count int
	err := c.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE agent_id = $1`, agentID).Scan(&count)
	if err != nil {
		return 0, err
	}

	if count <= c.maxPerAgent {
		return 0, nil
	}

	excess := count - c.maxPerAgent
	// Delete lowest scoring memories (importance * (access_count + 1))
	tag, err := c.pool.Exec(ctx, `
		DELETE FROM memories WHERE id IN (
			SELECT id FROM memories
			WHERE agent_id = $1
			ORDER BY importance_score * (COALESCE(access_count, 0) + 1) ASC
			LIMIT $2
		)
	`, agentID, excess)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ConsolidateAllAgents runs consolidation for all agents that have memories.
func (c *Consolidator) ConsolidateAllAgents(ctx context.Context) error {
	rows, err := c.pool.Query(ctx, `SELECT DISTINCT agent_id FROM memories`)
	if err != nil {
		return fmt.Errorf("listing agents with memories: %w", err)
	}
	defer rows.Close()

	var agentIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			continue
		}
		agentIDs = append(agentIDs, id)
	}

	for _, agentID := range agentIDs {
		if err := c.Consolidate(ctx, agentID); err != nil {
			slog.Warn("consolidation failed for agent", "agent_id", agentID, "error", err)
		}
	}

	return nil
}

package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Manager orchestrates the three-layer memory system:
// 1. Episodic (conversation) memories — time-decayed
// 2. Semantic (knowledge) memories — pgvector hybrid search
// 3. Procedural (skill) memories — access-count weighted
type Manager struct {
	episodic   *EpisodicStore
	semantic   *SemanticStore
	procedural *ProceduralStore
	pool       *pgxpool.Pool
	embedder   EmbeddingGeneratorFunc
}

// NewManager creates a new memory manager.
func NewManager(pool *pgxpool.Pool, episodic *EpisodicStore, semantic *SemanticStore, procedural *ProceduralStore, embedder EmbeddingGeneratorFunc) *Manager {
	return &Manager{
		pool:       pool,
		episodic:   episodic,
		semantic:   semantic,
		procedural: procedural,
		embedder:   embedder,
	}
}

// Recall retrieves relevant memories for the given context.
// Generates the query embedding once and shares it across all three stores.
func (m *Manager) Recall(ctx context.Context, agentID uuid.UUID, query string, limit int) ([]MemoryEntry, error) {
	if m.embedder == nil {
		return nil, nil
	}

	// Generate embedding once for all stores
	embedding, err := m.embedder(ctx, query)
	if err != nil {
		slog.Warn("failed to generate query embedding for recall", "error", err)
		return nil, nil
	}

	var results []MemoryEntry

	// Distribute limit across memory types
	perType := limit / 3
	if perType < 1 {
		perType = 1
	}

	// Search across all memory types with shared embedding
	episodic, err := m.episodic.SearchByVector(ctx, agentID, embedding, perType)
	if err == nil {
		results = append(results, episodic...)
	}

	semantic, err := m.semantic.SearchByVector(ctx, agentID, embedding, perType)
	if err == nil {
		results = append(results, semantic...)
	}

	procedural, err := m.procedural.SearchByVector(ctx, agentID, embedding, perType)
	if err == nil {
		results = append(results, procedural...)
	}

	return results, nil
}

// Store saves a new memory entry to the appropriate store.
func (m *Manager) Store(ctx context.Context, entry *MemoryEntry) error {
	switch entry.MemoryType {
	case "episodic":
		return m.episodic.Store(ctx, entry)
	case "semantic":
		return m.semantic.Store(ctx, entry)
	case "procedural":
		return m.procedural.Store(ctx, entry)
	default:
		return fmt.Errorf("unknown memory type: %s", entry.MemoryType)
	}
}

// Delete removes a memory entry by ID and agent ID.
func (m *Manager) Delete(ctx context.Context, memoryID, agentID uuid.UUID) error {
	_, err := m.pool.Exec(ctx, `
		DELETE FROM memories WHERE id = $1 AND agent_id = $2
	`, memoryID, agentID)
	return err
}

// List retrieves memories for an agent, optionally filtered by type.
func (m *Manager) List(ctx context.Context, agentID uuid.UUID, memoryType string, limit, offset int) ([]MemoryEntry, error) {
	query := `
		SELECT id, agent_id, tenant_id, memory_type, content, importance_score, created_at
		FROM memories
		WHERE agent_id = $1
	`
	args := []any{agentID}

	if memoryType != "" {
		query += ` AND memory_type = $2`
		args = append(args, memoryType)
	}

	query += ` ORDER BY created_at DESC LIMIT $` + fmt.Sprintf("%d", len(args)+1) + ` OFFSET $` + fmt.Sprintf("%d", len(args)+2)
	args = append(args, limit, offset)

	rows, err := m.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing memories: %w", err)
	}
	defer rows.Close()

	var results []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		if err := rows.Scan(&e.ID, &e.AgentID, &e.TenantID, &e.MemoryType, &e.Content, &e.ImportanceScore, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning memory: %w", err)
		}
		results = append(results, e)
	}
	return results, nil
}

// MemoryEntry represents a single memory record.
type MemoryEntry struct {
	ID              uuid.UUID `json:"id"`
	AgentID         uuid.UUID `json:"agent_id"`
	TenantID        uuid.UUID `json:"tenant_id"`
	MemoryType      string    `json:"memory_type"`
	Content         string    `json:"content"`
	ImportanceScore float64   `json:"importance_score"`
	SourceSessionID *uuid.UUID `json:"source_session_id,omitempty"`
	AccessCount     int       `json:"access_count,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
}

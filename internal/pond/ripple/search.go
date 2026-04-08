package ripple

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SearchEngine performs vector similarity search on agent memories.
type SearchEngine struct {
	pool       *pgxpool.Pool
	maxResults int
}

// NewSearchEngine creates a new vector search engine.
func NewSearchEngine(pool *pgxpool.Pool, maxResults int) *SearchEngine {
	if maxResults <= 0 {
		maxResults = 20
	}
	return &SearchEngine{
		pool:       pool,
		maxResults: maxResults,
	}
}

// SearchQuery contains the parameters for a memory search.
type SearchQuery struct {
	AgentID    uuid.UUID
	Embedding  []float32
	MemoryType string // optional filter
	Limit      int
}

// SearchResult contains a single search result with relevance score.
type SearchResult struct {
	ID              uuid.UUID `json:"id"`
	Content         string    `json:"content"`
	MemoryType      string    `json:"memory_type"`
	ImportanceScore float64   `json:"importance_score"`
	Similarity      float64   `json:"similarity"`
}

// Search performs vector similarity search with importance weighting.
func (e *SearchEngine) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = e.maxResults
	}

	embeddingStr := vectorToString(q.Embedding)

	query := `
		SELECT id, content, memory_type, importance_score,
		       1 - (embedding <=> $1::vector) as similarity
		FROM memories
		WHERE agent_id = $2
		  AND ($3 = '' OR memory_type = $3)
		  AND embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $4
	`

	rows, err := e.pool.Query(ctx, query, embeddingStr, q.AgentID, q.MemoryType, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.Content, &r.MemoryType, &r.ImportanceScore, &r.Similarity); err != nil {
			return nil, fmt.Errorf("scanning result: %w", err)
		}
		results = append(results, r)
	}

	return results, nil
}

// vectorToString converts a float32 slice to pgvector string format: '[0.1,0.2,...]'
func vectorToString(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

package ripple

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// IndexManager manages HNSW vector indexes.
type IndexManager struct {
	pool *pgxpool.Pool
}

// NewIndexManager creates a new HNSW index manager.
func NewIndexManager(pool *pgxpool.Pool) *IndexManager {
	return &IndexManager{pool: pool}
}

// EnsureIndex creates the HNSW index if it doesn't exist.
func (m *IndexManager) EnsureIndex(ctx context.Context, efConstruction, mParam int) error {
	query := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_memories_hnsw ON memories
		USING hnsw (embedding vector_cosine_ops)
		WITH (m = %d, ef_construction = %d)
	`, mParam, efConstruction)

	_, err := m.pool.Exec(ctx, query)
	return err
}

// SetSearchParams sets the runtime HNSW search parameters.
func (m *IndexManager) SetSearchParams(ctx context.Context, efSearch int) error {
	_, err := m.pool.Exec(ctx, fmt.Sprintf("SET hnsw.ef_search = %d", efSearch))
	return err
}

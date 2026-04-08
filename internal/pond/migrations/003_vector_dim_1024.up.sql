-- Migrate vector dimension from 1536 to 1024 for bge-m3 embedding model.

-- Drop the existing HNSW index
DROP INDEX IF EXISTS idx_memories_hnsw;

-- Alter column to 1024 dimensions (drop existing embeddings as they were generated with different model)
ALTER TABLE memories DROP COLUMN IF EXISTS embedding;
ALTER TABLE memories ADD COLUMN embedding vector(1024);

-- Recreate HNSW index with same parameters
CREATE INDEX idx_memories_hnsw
    ON memories USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 200);

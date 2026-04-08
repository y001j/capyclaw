-- Revert vector dimension back to 1536.
DROP INDEX IF EXISTS idx_memories_hnsw;
ALTER TABLE memories DROP COLUMN IF EXISTS embedding;
ALTER TABLE memories ADD COLUMN embedding vector(1536);
CREATE INDEX idx_memories_hnsw
    ON memories USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 200);

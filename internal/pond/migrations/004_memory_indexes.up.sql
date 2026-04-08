-- Indexes for memory consolidation and access-based queries.

-- Partial index for finding old, low-importance episodic memories for pruning
CREATE INDEX IF NOT EXISTS idx_memories_episodic_decay
    ON memories(agent_id, importance_score, created_at)
    WHERE memory_type = 'episodic';

-- Index for access-based ranking and consolidation
CREATE INDEX IF NOT EXISTS idx_memories_access
    ON memories(agent_id, access_count DESC);

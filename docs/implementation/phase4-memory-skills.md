# Phase 4: 记忆系统、上下文压缩、技能管理

## 目标

实现三层记忆系统（情景/语义/程序性），使 Agent 能跨 Session 记住信息；实现上下文窗口自动压缩，防止 token 溢出；完成技能安装/绑定/执行生命周期。

## 前置条件

- Phase 2 完成（Agent CRUD、Pipeline 核心阶段、LLM Provider）
- Phase 3 完成更佳（工具执行用于技能），但记忆系统可独立开发

## 当前状态

### 已实现
- `memory.Manager`（`internal/pond/memory/manager.go`）— Recall 方法在 3 层间分配搜索 ✓
- `memory.MemoryEntry` 类型定义 ✓
- `EpisodicStore/SemanticStore/ProceduralStore` 空壳结构体 + Search/Store 方法签名
- `ripple.SearchEngine.HybridSearch()`（`internal/pond/ripple/search.go`）— 完整的 RRF 混合查询 ✓
- `instinct.ContextManager`（`internal/burrow/instinct/context.go`）— ShouldCompact/RemainingTokens ✓
- `instinct.Compactor`（`internal/burrow/instinct/compaction.go`）— CompactAsync 框架，但 LLM 调用为 TODO
- `drift.Scheduler.EnqueueEmbedding()`（`internal/lodge/drift/scheduler.go`）— 任务入队 ✓
- `skills.Skill` 类型（`pkg/skills/skill.go`）— 完整的 manifest 字段定义 ✓
- `skills.Loader`（`pkg/skills/loader.go`）— 目录遍历框架，parseManifest 为 TODO
- `skills.Validate()`（`pkg/skills/validator.go`）— 字段校验 ✓
- DB schema: `memories` 表 + HNSW 索引 + FTS 索引 + `skills` 表 + `agent_skills` 表

### 需要实现
- 3 个 Memory Store 的实际 DB 操作
- Embedding 生成（调用 Embedding API）
- Asynq Worker 处理异步任务
- Compactor 实际 LLM 摘要调用
- Pipeline WorkspaceLoader 阶段（加载技能）
- 技能 YAML frontmatter 解析
- 所有技能 Handler
- 所有记忆 Handler

---

## 实施任务

### 1. 三层记忆 Store 实现

#### 1.1 EpisodicStore

**修改文件**: `internal/pond/memory/episodic.go`

```go
type EpisodicStore struct {
    pool *pgxpool.Pool
}

func NewEpisodicStore(pool *pgxpool.Pool) *EpisodicStore {
    return &EpisodicStore{pool: pool}
}

func (s *EpisodicStore) Search(ctx context.Context, agentID uuid.UUID, query string, limit int) ([]MemoryEntry, error) {
    // 情景记忆按时间衰减排序 + FTS 相关性
    rows, err := s.pool.Query(ctx, `
        SELECT id, agent_id, content, importance_score
        FROM memories
        WHERE agent_id = $1 AND memory_type = 'episodic'
          AND to_tsvector('english', content) @@ plainto_tsquery('english', $2)
        ORDER BY importance_score * EXP(-0.1 * EXTRACT(EPOCH FROM (NOW() - created_at)) / 86400) DESC
        LIMIT $3
    `, agentID, query, limit)
    // scan results...
}

func (s *EpisodicStore) Store(ctx context.Context, entry *MemoryEntry) error {
    _, err := s.pool.Exec(ctx, `
        INSERT INTO memories (agent_id, tenant_id, memory_type, content, importance_score, source_session_id)
        VALUES ($1, $2, 'episodic', $3, $4, $5)
    `, entry.AgentID, entry.TenantID, entry.Content, entry.ImportanceScore, entry.SourceSessionID)
    return err
}
```

#### 1.2 SemanticStore

**修改文件**: `internal/pond/memory/semantic.go`

```go
type SemanticStore struct {
    pool   *pgxpool.Pool
    search *ripple.SearchEngine
}

func NewSemanticStore(pool *pgxpool.Pool, search *ripple.SearchEngine) *SemanticStore {
    return &SemanticStore{pool: pool, search: search}
}

func (s *SemanticStore) Search(ctx context.Context, agentID uuid.UUID, query string, limit int) ([]MemoryEntry, error) {
    // 使用已有的 HybridSearch（向量 + FTS + RRF）
    // 需要先获取 query 的 embedding（这里先用 FTS-only 降级搜索）
    results, err := s.search.HybridSearch(ctx, ripple.SearchQuery{
        AgentID:    agentID,
        Query:      query,
        MemoryType: "semantic",
        Limit:      limit,
    })
    // 转换 SearchResult → MemoryEntry
}

func (s *SemanticStore) Store(ctx context.Context, entry *MemoryEntry) error {
    // 1. INSERT 记忆（embedding 为 NULL）
    // 2. 异步入队 embedding 生成任务
    _, err := s.pool.Exec(ctx, `
        INSERT INTO memories (agent_id, tenant_id, memory_type, content, importance_score)
        VALUES ($1, $2, 'semantic', $3, $4)
        RETURNING id
    `, ...)
    // scheduler.EnqueueEmbedding(memoryID, content)
}
```

#### 1.3 ProceduralStore

**修改文件**: `internal/pond/memory/procedural.go`

```go
type ProceduralStore struct {
    pool *pgxpool.Pool
}

func (s *ProceduralStore) Search(ctx context.Context, agentID uuid.UUID, query string, limit int) ([]MemoryEntry, error) {
    // 程序性记忆按 access_count * importance_score 排序
    rows, err := s.pool.Query(ctx, `
        SELECT id, agent_id, content, importance_score
        FROM memories
        WHERE agent_id = $1 AND memory_type = 'procedural'
        ORDER BY access_count * importance_score DESC
        LIMIT $2
    `, agentID, limit)
    // ...
}

func (s *ProceduralStore) Store(ctx context.Context, entry *MemoryEntry) error {
    // INSERT with memory_type = 'procedural'
}
```

### 2. Embedding 生成

**新建文件**: `internal/pond/ripple/embedding.go`

```go
type EmbeddingGenerator struct {
    apiKey   string
    baseURL  string
    model    string // "text-embedding-3-small"
    client   *http.Client
}

func NewEmbeddingGenerator(apiKey, baseURL, model string) *EmbeddingGenerator {
    return &EmbeddingGenerator{
        apiKey:  apiKey,
        baseURL: baseURL,
        model:   model,
        client:  &http.Client{Timeout: 30 * time.Second},
    }
}

func (g *EmbeddingGenerator) Generate(ctx context.Context, text string) ([]float32, error) {
    // POST /v1/embeddings
    // body: {"model": "text-embedding-3-small", "input": text}
    // 返回 1536 维向量
}
```

### 3. Asynq Worker

**新建文件**: `internal/lodge/drift/worker.go`

```go
type Worker struct {
    server    *asynq.Server
    embedding *ripple.EmbeddingGenerator
    pool      *pgxpool.Pool
}

func NewWorker(redisAddr string, embedding *ripple.EmbeddingGenerator, pool *pgxpool.Pool) *Worker {
    srv := asynq.NewServer(
        asynq.RedisClientOpt{Addr: redisAddr},
        asynq.Config{Concurrency: 10},
    )
    return &Worker{server: srv, embedding: embedding, pool: pool}
}

func (w *Worker) Start() error {
    mux := asynq.NewServeMux()
    mux.HandleFunc(TypeEmbeddingGenerate, w.handleEmbeddingGenerate)
    mux.HandleFunc(TypeMemoryCompact, w.handleMemoryCompact)
    return w.server.Start(mux)
}

func (w *Worker) handleEmbeddingGenerate(ctx context.Context, task *asynq.Task) error {
    var payload struct {
        MemoryID string `json:"memory_id"`
        Content  string `json:"content"`
    }
    json.Unmarshal(task.Payload(), &payload)

    // 1. 生成 embedding
    vector, err := w.embedding.Generate(ctx, payload.Content)

    // 2. 更新数据库
    _, err = w.pool.Exec(ctx,
        "UPDATE memories SET embedding = $1 WHERE id = $2",
        pgvector.NewVector(vector), payload.MemoryID,
    )
    return err
}
```

### 4. 上下文压缩实现

**修改文件**: `internal/burrow/instinct/compaction.go`

```go
func (c *Compactor) CompactAsync(ctx context.Context, sessionID string, messages []string, llmClient llm.Client) <-chan *CompactionResult {
    ch := make(chan *CompactionResult, 1)

    go func() {
        defer close(ch)

        // 1. 构造摘要请求
        summaryPrompt := "请将以下对话历史压缩为简洁的摘要，保留所有关键事实、决策和上下文信息：\n\n" +
            strings.Join(messages, "\n")

        llmReq := &llm.Request{
            Model:     c.summaryModel,
            Messages:  []llm.Message{{Role: "user", Content: summaryPrompt}},
            MaxTokens: 2000,
        }

        // 2. 调用 LLM 生成摘要
        chunks, err := llmClient.Stream(ctx, llmReq)
        if err != nil {
            ch <- &CompactionResult{Error: err}
            return
        }

        var summary strings.Builder
        var tokensSaved int
        for chunk := range chunks {
            if chunk.Type == "text" {
                summary.WriteString(chunk.Content)
            }
        }

        // 3. 更新 session.context_summary
        // 4. 计算节省的 token 数

        ch <- &CompactionResult{
            Summary:           summary.String(),
            TokensSaved:       tokensSaved,
            MessagesCompacted: len(messages),
        }
    }()

    return ch
}
```

**在 Pipeline 中集成压缩检查**：在 PromptBuilder 或 SessionPersister 阶段之后检查 `ContextManager.ShouldCompact()`，如果超阈值则触发 `CompactAsync`。

### 5. 技能 YAML 解析

**修改文件**: `pkg/skills/loader.go`

```go
import "gopkg.in/yaml.v3"

func parseManifest(path string) (*Skill, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }

    // 解析 YAML frontmatter (--- 分隔)
    parts := bytes.SplitN(data, []byte("---\n"), 3)
    if len(parts) < 3 {
        return nil, fmt.Errorf("invalid skill format: missing YAML frontmatter")
    }

    var skill Skill
    if err := yaml.Unmarshal(parts[1], &skill); err != nil {
        return nil, fmt.Errorf("parsing skill manifest: %w", err)
    }

    skill.SourcePath = filepath.Dir(path)
    return &skill, Validate(&skill)
}
```

### 6. 技能 Handler

**修改文件**: `internal/riverbank/handlers.go`

```go
func (s *Server) handleInstallSkill(w http.ResponseWriter, r *http.Request) {
    // 1. 解析请求体：{source: "capyhub", name: "xxx"} 或 {source: "url", url: "..."}
    // 2. 下载/解析技能 manifest
    // 3. skills.Validate()
    // 4. INSERT INTO skills (...) VALUES (...)
    // 5. 返回 201 + Skill JSON
}

func (s *Server) handleAttachSkill(w http.ResponseWriter, r *http.Request) {
    agentID := chi.URLParam(r, "agentID")
    // 1. 解析 {skill_id, priority, config}
    // 2. INSERT INTO agent_skills (agent_id, skill_id, priority, config) VALUES (...)
    // 3. 返回 200
}

func (s *Server) handleDetachSkill(w http.ResponseWriter, r *http.Request) {
    agentID := chi.URLParam(r, "agentID")
    skillID := chi.URLParam(r, "skillID")
    // DELETE FROM agent_skills WHERE agent_id = $1 AND skill_id = $2
}

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
    // SELECT * FROM skills WHERE tenant_id = $1 ORDER BY name
}

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
    agentID := chi.URLParam(r, "agentID")
    memoryType := r.URL.Query().Get("type") // optional filter
    // SELECT from memories WHERE agent_id = $1 AND ($2 = '' OR memory_type = $2)
}

func (s *Server) handleSearchMemories(w http.ResponseWriter, r *http.Request) {
    // POST body: {query: "...", limit: 20}
    // 调用 memory.Manager.Recall()
}

func (s *Server) handleCreateMemory(w http.ResponseWriter, r *http.Request) {
    // 手动创建记忆: {type, content, importance_score}
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
    // DELETE FROM memories WHERE id = $1 AND agent_id = $2
}
```

### 7. Pipeline WorkspaceLoader

**修改文件**: `internal/burrow/pipeline.go`

```go
type WorkspaceLoader struct {
    pool *pgxpool.Pool
}

func (s *WorkspaceLoader) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
    // 1. 加载 Agent 绑定的技能
    rows, err := s.pool.Query(ctx, `
        SELECT s.name, s.description, s.content_md, s.manifest
        FROM skills s
        JOIN agent_skills as_on s.id = as.skill_id
        WHERE as.agent_id = $1 AND as.enabled = true
        ORDER BY as.priority DESC
    `, req.AgentID)

    // 2. 将技能转为 instinct.SkillPrompt 列表
    // 3. 注入到 req 的可用技能列表

    // 4. 调用 memory.Manager.Recall() 获取相关记忆
    // 5. 将记忆注入到 req 的上下文中

    return req, nil
}
```

### 8. Memory Manager 增强

**修改文件**: `internal/pond/memory/manager.go`

增加 `Store` 和 `Delete` 方法：

```go
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
```

---

## 关键文件清单

| 文件 | 操作 |
|------|------|
| `internal/pond/memory/episodic.go` | **实现** — DB 操作 |
| `internal/pond/memory/semantic.go` | **实现** — pgvector 搜索 |
| `internal/pond/memory/procedural.go` | **实现** — DB 操作 |
| `internal/pond/memory/manager.go` | **增强** — Store/Delete |
| `internal/pond/ripple/embedding.go` | **新建** — Embedding API |
| `internal/lodge/drift/worker.go` | **新建** — Asynq 异步 Worker |
| `internal/burrow/instinct/compaction.go` | **实现** — LLM 摘要 |
| `internal/burrow/pipeline.go` | **实现** — WorkspaceLoader |
| `internal/riverbank/handlers.go` | **实现** — Memory + Skill CRUD |
| `pkg/skills/loader.go` | **实现** — YAML 解析 |

## 可复用的已有代码

- `ripple.SearchEngine.HybridSearch()` — RRF 混合搜索（`search.go:48-104`）
- `instinct.ContextManager.ShouldCompact()` — 压缩阈值判断（`context.go:18-20`）
- `instinct.PromptBuilder.Build()` — 支持 SkillPrompt XML 格式化（`builder.go:54-67`）
- `skills.Validate()` — manifest 校验（`validator.go:7-31`）
- `drift.Scheduler.EnqueueEmbedding()` — 任务入队（`scheduler.go:33-45`）
- `memory.Manager.Recall()` — 多层搜索合并（`manager.go:29-49`）

## 验收标准

1. **记忆搜索**: `POST /api/v1/agents/{id}/memories/search` 返回按 RRF 分数排序的结果
2. **上下文压缩**: 持续对话到 ~160k tokens → 触发压缩 → sessions.context_summary 更新 → 压缩后仍能引用早期信息
3. **Embedding 异步生成**: 创建记忆后，embedding 列在几秒内由 Worker 填充
4. **技能安装**: `POST /api/v1/skills/install` 安装技能 → 出现在 skills 表
5. **技能绑定**: 绑定技能到 Agent → 对话中 Agent 可使用技能的工具
6. **技能解绑**: 解绑后工具不再出现

## 验证方法

```bash
# 记忆操作
curl -X POST http://localhost:18789/api/v1/agents/<id>/memories \
  -H "Authorization: Bearer <token>" \
  -d '{"type":"semantic","content":"用户是一名 Go 后端开发者","importance_score":0.8}'

curl -X POST http://localhost:18789/api/v1/agents/<id>/memories/search \
  -H "Authorization: Bearer <token>" \
  -d '{"query":"用户的技术栈","limit":5}'

# 查看 embedding 是否生成
psql -c "SELECT id, embedding IS NOT NULL as has_embedding FROM memories"

# 技能安装绑定
curl -X POST http://localhost:18789/api/v1/skills/install \
  -H "Authorization: Bearer <token>" \
  -d '{"source":"upload","name":"calculator"}'

curl -X POST http://localhost:18789/api/v1/agents/<id>/skills \
  -H "Authorization: Bearer <token>" \
  -d '{"skill_id":"<skill-id>"}'
```

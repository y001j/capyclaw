# Phase 2: Agent CRUD 与核心对话循环

## 目标

完成 Agent 生命周期管理（创建/查询/更新/删除），实现端到端对话流程：用户发送 HTTP 请求 → LLM 流式响应 → 消息持久化到 PostgreSQL。这是整个平台的核心价值路径。

## 前置条件

- Phase 1 完成（Server 依赖注入、JWT 认证、错误处理框架）

## 当前状态

### 已实现
- `AgentRepository` CRUD（`internal/pond/pebble/agent_repo.go`）— Create/Get/ListByTenant/Delete 已完成
- `SessionRepository`（`internal/pond/pebble/session_repo.go`）— GetOrCreate/Get/ListByAgent 已完成
- `MessageRepository`（`internal/pond/pebble/message_repo.go`）— Append/ListBySession 已完成
- `Pipeline` 框架（`internal/burrow/pipeline.go`）— 8 阶段定义 + 执行循环 + 流式 channel
- `ModelSelector` 阶段（pipeline.go:117-123）— 基础模型选择（使用 agent config 或默认值）
- `PromptBuilder`（`internal/burrow/instinct/builder.go`）— 完整的系统提示组装
- `llm.Client` 接口（`internal/burrow/llm/client.go`）— Request/Chunk/Usage 类型定义
- `FailoverClient`（`internal/burrow/llm/failover.go`）— 带断路器的多 Provider 故障转移
- `SSEParser`（`internal/burrow/llm/streaming.go`）— SSE 解析 + OpenAI 格式 Chunk 解析

### 需要实现
- 所有 Handler 返回 501（`internal/riverbank/handlers.go`）
- Anthropic Provider 为空壳（`internal/burrow/llm/providers/anthropic.go:25-29`）
- OpenAI Provider 为空壳
- Pipeline 阶段 SessionResolver/PromptBuilder/LLMInvoker/SessionPersister 为 TODO
- Chat Completions SSE 流式 Handler 未实现
- Agent Create 缺少完整字段（repo 只存基础字段，缺少 system_prompt/identity_md 等）

---

## 实施任务

### 1. 增强 AgentRepository

**修改文件**: `internal/pond/pebble/agent_repo.go`

当前 `Agent` struct 只有 Name/Slug/Model/Status，缺少数据库 schema 中的完整字段：

```go
type Agent struct {
    ID             uuid.UUID         `json:"id"`
    TenantID       uuid.UUID         `json:"tenant_id"`
    Name           string            `json:"name"`
    Slug           string            `json:"slug"`
    Model          string            `json:"model"`
    FallbackModels []string          `json:"fallback_models"`
    SystemPrompt   *string           `json:"system_prompt"`
    IdentityMD     *string           `json:"identity_md"`
    SoulMD         *string           `json:"soul_md"`
    UserMD         *string           `json:"user_md"`
    Settings       json.RawMessage   `json:"settings"`
    ToolPolicy     json.RawMessage   `json:"tool_policy"`
    Status         string            `json:"status"`
    CreatedAt      time.Time         `json:"created_at"`
    UpdatedAt      time.Time         `json:"updated_at"`
}
```

增加方法：
- `Create()` — 扩展 INSERT 支持所有字段，RETURNING 返回完整记录
- `Update()` — PATCH 语义的部分更新
- `SoftDelete()` — 设置 status='archived' 而非硬删除

### 2. Agent CRUD Handler

**修改文件**: `internal/riverbank/handlers.go`

```go
func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
    // 1. 解析请求体 JSON
    // 2. 从 context 获取 tenant_id
    // 3. 验证必填字段（name, slug）
    // 4. 调用 s.agentRepo.Create()
    // 5. 记录审计日志: s.auditLogger.Log(ctx, &footprint.AuditEvent{...})
    // 6. 返回 201 + Agent JSON
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
    // 1. chi.URLParam(r, "agentID") 获取 Agent ID
    // 2. s.agentRepo.Get(ctx, id)
    // 3. 404 或返回 Agent JSON
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
    // 1. 从 context 获取 tenant_id
    // 2. 解析 ?limit=&offset= 分页参数
    // 3. s.agentRepo.ListByTenant(ctx, tenantID)
    // 4. 返回 JSON 数组
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
    // PATCH 语义：只更新请求体中包含的字段
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
    // 软删除：status → 'archived'
    // 审计日志
}
```

### 3. Anthropic Provider 实现

**修改文件**: `internal/burrow/llm/providers/anthropic.go`

这是最关键的实现之一，需要支持 Anthropic Messages API 的 SSE 流式响应：

```go
type AnthropicClient struct {
    apiKey     string
    baseURL    string
    httpClient *http.Client
}

func (c *AnthropicClient) Stream(ctx context.Context, req *llm.Request) (<-chan *llm.Chunk, error) {
    // 1. 构造 Anthropic 请求体
    body := map[string]any{
        "model":      req.Model,
        "max_tokens": req.MaxTokens,
        "stream":     true,
        "messages":   convertMessages(req.Messages),
    }
    if len(req.Tools) > 0 {
        body["tools"] = convertTools(req.Tools)
    }

    // 2. 发送 HTTP POST
    httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", ...)
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("x-api-key", c.apiKey)
    httpReq.Header.Set("anthropic-version", "2023-06-01")

    resp, err := c.httpClient.Do(httpReq)

    // 3. 解析 SSE 流
    ch := make(chan *llm.Chunk, 64)
    go func() {
        defer close(ch)
        defer resp.Body.Close()

        parser := llm.NewSSEParser(resp.Body)
        for event := range parser.Parse(ctx) {
            chunk := c.parseAnthropicEvent(event)
            if chunk != nil {
                ch <- chunk
            }
        }
    }()
    return ch, nil
}
```

**Anthropic SSE 事件解析**：
- `message_start` → 提取 usage.input_tokens
- `content_block_start` → 识别 text 或 tool_use block
- `content_block_delta` → type=text_delta 或 input_json_delta
- `content_block_stop` → 结束当前 block
- `message_delta` → 提取 stop_reason, usage.output_tokens
- `message_stop` → 发送 done chunk

**Token 计数**：
```go
func (c *AnthropicClient) CountTokens(ctx context.Context, messages []llm.Message) (int, error) {
    // POST /v1/messages/count_tokens
}
```

### 4. OpenAI Provider 实现

**修改文件**: `internal/burrow/llm/providers/openai.go`

```go
func (c *OpenAIClient) Stream(ctx context.Context, req *llm.Request) (<-chan *llm.Chunk, error) {
    // 1. 构造 OpenAI Chat Completion 请求
    // 2. POST /v1/chat/completions with stream=true
    // 3. 复用 llm.ParseOpenAIChunk() 解析 SSE data 行
}
```

`ParseOpenAIChunk` 已在 `streaming.go:78-104` 实现，直接复用。

### 5. Pipeline 核心阶段实现

**修改文件**: `internal/burrow/pipeline.go`

#### 5.1 SessionResolver

```go
func (s *SessionResolver) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
    sessionKey := req.Message.SessionKey
    if sessionKey == "" {
        sessionKey = uuid.New().String() // 自动创建 session
    }
    // 调用 sessionRepo.GetOrCreate(ctx, req.AgentID, req.TenantID, sessionKey)
    // 将结果存入 req.Session
}
```

**注意**: Pipeline stages 需要访问 Repo，所以 SessionResolver 需要持有 `*pebble.SessionRepository` 引用。修改 `NewPipeline` 接收依赖参数。

#### 5.2 PromptBuilder

```go
func (s *PromptBuilder) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
    builder := &instinct.PromptBuilder{}
    prompt, err := builder.Build(ctx, &instinct.BuildRequest{
        IdentityMD:     req.AgentConfig.IdentityMD,
        SoulMD:         req.AgentConfig.SoulMD,
        UserMD:         req.AgentConfig.UserMD,
        ContextSummary: req.Session.ContextSummary,
    })
    req.SystemPrompt = prompt

    // 加载历史消息
    // messages, _ := messageRepo.ListBySession(ctx, req.Session.ID, 100, 0)
    // 转换为 req.Messages
}
```

#### 5.3 LLMInvoker

```go
func (s *LLMInvoker) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
    // 1. 根据 req.Model 前缀选择 Provider
    //    claude-* → Anthropic, gpt-* → OpenAI
    // 2. 构造 llm.Request
    // 3. 调用 client.Stream(ctx, llmReq)
    // 4. 从 channel 读取 chunks 并转发到 pipeline 输出 channel
    // 5. 收集 Usage 统计
}
```

**重要设计**: LLMInvoker 需要向外部发送流式数据，但 Pipeline.Execute 的 channel 在主 goroutine 中管理。需要修改 Pipeline 执行模型：
- 选项 A: LLMInvoker 直接写入外部 channel（需要传入）
- 选项 B: PipelineRequest 中增加 `OutputCh chan *StreamChunk`

#### 5.4 SessionPersister

```go
func (s *SessionPersister) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
    // 1. 持久化用户消息
    userMsg := &pebble.Message{
        SessionID: req.Session.ID,
        TenantID:  req.TenantID,
        Role:      "user",
        Content:   json.RawMessage(`"` + req.Message.Content + `"`),
    }
    messageRepo.Append(ctx, userMsg)

    // 2. 持久化助手响应
    assistantMsg := &pebble.Message{...}
    messageRepo.Append(ctx, assistantMsg)

    // 3. 更新 session token 计数
    // UPDATE sessions SET total_input_tokens = total_input_tokens + $1, ...
}
```

### 6. Pipeline 依赖注入

**修改文件**: `internal/burrow/pipeline.go`

```go
type PipelineDeps struct {
    SessionRepo *pebble.SessionRepository
    MessageRepo *pebble.MessageRepository
    LLMClient   llm.Client // FailoverClient
    Config      *config.BurrowConfig
}

func NewPipeline(deps *PipelineDeps) *Pipeline {
    return &Pipeline{
        stages: []Stage{
            &SessionResolver{sessionRepo: deps.SessionRepo},
            &WorkspaceLoader{},
            &ModelSelector{defaultModel: deps.Config.DefaultModel},
            &PromptBuilder{messageRepo: deps.MessageRepo},
            &ToolPolicyFilter{},
            &LLMInvoker{client: deps.LLMClient},
            &ToolExecutor{},
            &SessionPersister{
                sessionRepo: deps.SessionRepo,
                messageRepo: deps.MessageRepo,
            },
        },
    }
}
```

### 7. Chat Completions Handler

**修改文件**: `internal/riverbank/handlers.go`

实现 OpenAI 兼容的 `/v1/chat/completions` 端点：

```go
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
    // 1. 解析请求体（OpenAI 格式）
    var req struct {
        Model     string    `json:"model"`
        Messages  []Message `json:"messages"`
        Stream    bool      `json:"stream"`
        AgentID   string    `json:"agent_id"`   // CapyClaw 扩展字段
        SessionKey string   `json:"session_key"` // CapyClaw 扩展字段
    }
    json.NewDecoder(r.Body).Decode(&req)

    // 2. 构造 IncomingMessage
    lastMsg := req.Messages[len(req.Messages)-1]
    incoming := &burrow.IncomingMessage{
        SessionKey: req.SessionKey,
        Content:    lastMsg.Content,
        Role:       lastMsg.Role,
        UserID:     r.Context().Value(middleware.UserIDKey).(string),
    }

    // 3. 获取或创建 Agent runtime
    // 4. 调用 pipeline.Execute(ctx, pipelineReq)

    // 5. 流式响应
    if req.Stream {
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")

        flusher := w.(http.Flusher)
        for chunk := range outputCh {
            // 转换为 OpenAI SSE 格式
            data, _ := json.Marshal(toOpenAIChunk(chunk))
            fmt.Fprintf(w, "data: %s\n\n", data)
            flusher.Flush()
        }
        fmt.Fprint(w, "data: [DONE]\n\n")
        flusher.Flush()
    } else {
        // 非流式：收集所有 chunks 合并返回
    }
}
```

### 8. Session/Message Handler

**修改文件**: `internal/riverbank/handlers.go`

```go
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
    id, _ := uuid.Parse(chi.URLParam(r, "sessionID"))
    session, err := s.sessionRepo.Get(r.Context(), id)
    // 错误处理 + 返回 JSON
}

func (s *Server) handleListAgentSessions(w http.ResponseWriter, r *http.Request) {
    agentID, _ := uuid.Parse(chi.URLParam(r, "agentID"))
    limit, offset := parsePagination(r)
    sessions, err := s.sessionRepo.ListByAgent(r.Context(), agentID, limit, offset)
    // 返回 JSON 数组
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
    sessionID, _ := uuid.Parse(chi.URLParam(r, "sessionID"))
    limit, offset := parsePagination(r)
    messages, err := s.messageRepo.ListBySession(r.Context(), sessionID, limit, offset)
    // 返回 JSON 数组
}
```

### 9. LLM Provider 工厂

**新建文件**: `internal/burrow/llm/factory.go`

```go
func NewClientFromConfig(cfg *config.ProvidersConfig, fallbackModels []string) llm.Client {
    var clients []llm.Client

    if cfg.Anthropic.APIKey != "" {
        clients = append(clients, providers.NewAnthropicClient(
            cfg.Anthropic.APIKey, cfg.Anthropic.BaseURL,
        ))
    }
    if cfg.OpenAI.APIKey != "" {
        clients = append(clients, providers.NewOpenAIClient(
            cfg.OpenAI.APIKey, cfg.OpenAI.BaseURL,
        ))
    }

    return llm.NewFailoverClient(clients...)
}
```

---

## 关键文件清单

| 文件 | 操作 |
|------|------|
| `internal/pond/pebble/agent_repo.go` | **增强** — 完整字段 + Update |
| `internal/riverbank/handlers.go` | **实现** — Agent CRUD + Chat + Session/Message 查询 |
| `internal/burrow/llm/providers/anthropic.go` | **实现** — Messages API 流式调用 |
| `internal/burrow/llm/providers/openai.go` | **实现** — Chat Completions 流式调用 |
| `internal/burrow/pipeline.go` | **实现** — 4 个核心 Stage + 依赖注入 |
| `internal/burrow/llm/factory.go` | **新建** — Provider 工厂 |

## 可复用的已有代码

- `llm.SSEParser.Parse()` — SSE 事件流解析（`streaming.go`）
- `llm.ParseOpenAIChunk()` — OpenAI 格式解析（`streaming.go:78-104`）
- `llm.FailoverClient` — 断路器故障转移（`failover.go`）
- `instinct.PromptBuilder.Build()` — 系统提示组装（`builder.go`）
- `pebble.SessionRepository.GetOrCreate()` — Session UPSERT（`session_repo.go:39-58`）
- `pebble.MessageRepository.Append()` — 消息写入（`message_repo.go:38-48`）

## 验收标准

1. **Agent CRUD**:
   - `POST /api/v1/agents` 创建 Agent → 201
   - `GET /api/v1/agents` 返回租户下所有 Agent
   - `GET /api/v1/agents/{id}` 返回 Agent 详情
   - `PATCH /api/v1/agents/{id}` 更新模型/提示 → 200
   - `DELETE /api/v1/agents/{id}` 软删除 → 200

2. **对话**:
   - `POST /v1/chat/completions` with `stream: true` 返回 SSE 流，包含 Claude 的响应
   - 流结束后 `data: [DONE]` 标记
   - messages 表中出现 user + assistant 消息记录
   - sessions 表中 token 计数更新

3. **故障转移**:
   - Anthropic API 不可用时，断路器跳闸，回退到 OpenAI（如配置了 OpenAI API key）

## 验证方法

```bash
# 创建 Agent
curl -X POST http://localhost:18789/api/v1/agents \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Agent","slug":"test-agent","model":"claude-sonnet-4-20250514"}'

# 发送消息（流式）
curl -N -X POST http://localhost:18789/v1/chat/completions \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-20250514","stream":true,"agent_id":"<id>","messages":[{"role":"user","content":"Hello, who are you?"}]}'

# 查看消息历史
curl http://localhost:18789/api/v1/sessions/<session-id>/messages \
  -H "Authorization: Bearer <token>"
```

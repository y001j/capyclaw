# Phase 6: 渠道适配器、定时任务、Temporal 工作流

## 目标

让 Agent 可通过 Telegram、Discord、Slack 等第三方平台接收和回复消息；实现 Cron 定时触发 Agent 执行任务；集成 Temporal 实现持久化、可恢复的长期工作流。

## 前置条件

- Phase 2 完成（对话循环、Pipeline）

## 当前状态

### 已实现
- `adapters.ChannelAdapter` 接口（`internal/riverbank/adapters/adapter.go`）— Start/Stop/SendMessage + IncomingMessage/OutgoingMessage 类型 ✓
- DB schema: `channel_connections` 表 + `agent_bindings` 表 ✓
- DB schema: `cron_jobs` 表 ✓
- `drift.Scheduler`（`internal/lodge/drift/scheduler.go`）— Asynq 客户端 + EnqueueEmbedding ✓
- 任务类型常量: `TypeCronExecute` 已定义

### 需要实现
- Telegram/Discord/Slack 适配器实现
- 通用 Webhook Handler
- 适配器到 Pipeline 的消息路由
- Cron 调度器和执行器
- Temporal SDK 集成
- 对应的 API Handler

---

## 实施任务

### 1. Telegram 适配器

**新建文件**: `internal/riverbank/adapters/telegram/telegram.go`

```go
import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

type TelegramAdapter struct {
    bot     *tgbotapi.BotAPI
    handler adapters.MessageHandler
    bindings map[int64]uuid.UUID // chat_id → agent_id mapping
}

func NewTelegramAdapter(token string, handler adapters.MessageHandler) (*TelegramAdapter, error) {
    bot, err := tgbotapi.NewBotAPI(token)
    // ...
}

func (t *TelegramAdapter) Start(ctx context.Context) error {
    updateConfig := tgbotapi.NewUpdate(0)
    updateConfig.Timeout = 60
    updates := t.bot.GetUpdatesChan(updateConfig)

    for {
        select {
        case <-ctx.Done():
            return nil
        case update := <-updates:
            if update.Message == nil {
                continue
            }
            go t.handleMessage(ctx, update.Message)
        }
    }
}

func (t *TelegramAdapter) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
    // 1. 从 agent_bindings 表查找 chat_id 绑定的 agent
    // 2. 构造 adapters.IncomingMessage
    incoming := &adapters.IncomingMessage{
        ChannelType: "telegram",
        ChannelID:   fmt.Sprintf("%d", msg.Chat.ID),
        SenderID:    fmt.Sprintf("%d", msg.From.ID),
        Content:     msg.Text,
        Timestamp:   time.Unix(int64(msg.Date), 0),
    }
    // 3. 调用 handler(incoming)
    // 4. 收集响应并通过 bot.Send() 回复
}

func (t *TelegramAdapter) SendMessage(channelID string, content adapters.OutgoingMessage) error {
    chatID, _ := strconv.ParseInt(channelID, 10, 64)
    msg := tgbotapi.NewMessage(chatID, content.Text)
    _, err := t.bot.Send(msg)
    return err
}

func (t *TelegramAdapter) Stop() error {
    t.bot.StopReceivingUpdates()
    return nil
}
```

### 2. Discord 适配器

**新建文件**: `internal/riverbank/adapters/discord/discord.go`

```go
import "github.com/bwmarrin/discordgo"

type DiscordAdapter struct {
    session *discordgo.Session
    handler adapters.MessageHandler
}

func NewDiscordAdapter(token string, handler adapters.MessageHandler) (*DiscordAdapter, error) {
    session, err := discordgo.New("Bot " + token)
    // ...
}

func (d *DiscordAdapter) Start(ctx context.Context) error {
    d.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
        if m.Author.ID == s.State.User.ID {
            return // 忽略自己的消息
        }
        go d.handleMessage(ctx, m)
    })
    return d.session.Open()
}

func (d *DiscordAdapter) handleMessage(ctx context.Context, m *discordgo.MessageCreate) {
    incoming := &adapters.IncomingMessage{
        ChannelType: "discord",
        ChannelID:   m.ChannelID,
        SenderID:    m.Author.ID,
        Content:     m.Content,
    }
    // 路由到 agent，获取响应，回复到 channel
}

func (d *DiscordAdapter) SendMessage(channelID string, content adapters.OutgoingMessage) error {
    _, err := d.session.ChannelMessageSend(channelID, content.Text)
    return err
}
```

### 3. Slack 适配器

**新建文件**: `internal/riverbank/adapters/slack/slack.go`

```go
import (
    "github.com/slack-go/slack"
    "github.com/slack-go/slack/socketmode"
)

type SlackAdapter struct {
    client  *slack.Client
    socket  *socketmode.Client
    handler adapters.MessageHandler
}

func NewSlackAdapter(botToken, appToken string, handler adapters.MessageHandler) (*SlackAdapter, error) {
    client := slack.New(botToken, slack.OptionAppLevelToken(appToken))
    socket := socketmode.New(client)
    return &SlackAdapter{client: client, socket: socket, handler: handler}, nil
}

func (s *SlackAdapter) Start(ctx context.Context) error {
    go func() {
        for evt := range s.socket.Events {
            if evt.Type == socketmode.EventTypeEventsAPI {
                // 处理消息事件
                // 在 thread 中回复
            }
        }
    }()
    return s.socket.Run()
}
```

### 4. 适配器管理器

**新建文件**: `internal/riverbank/adapters/manager.go`

```go
type AdapterManager struct {
    pool     *pgxpool.Pool
    pipeline *burrow.Pipeline
    adapters map[string]ChannelAdapter
}

func NewAdapterManager(pool *pgxpool.Pool, pipeline *burrow.Pipeline) *AdapterManager {
    return &AdapterManager{pool: pool, pipeline: pipeline, adapters: make(map[string]ChannelAdapter)}
}

func (m *AdapterManager) StartAll(ctx context.Context, cfg config.WhiskersConfig) error {
    if cfg.Telegram.Enabled {
        adapter, _ := telegram.NewTelegramAdapter(cfg.Telegram.BotToken, m.routeMessage)
        m.adapters["telegram"] = adapter
        go adapter.Start(ctx)
    }
    // Discord, Slack 类似...
}

func (m *AdapterManager) routeMessage(ctx context.Context, msg *IncomingMessage) (*OutgoingMessage, error) {
    // 1. 查 agent_bindings 表找到绑定的 agent
    // 2. 构造 PipelineRequest
    // 3. 执行 pipeline
    // 4. 收集结果返回
}
```

### 5. Webhook Handler

**修改文件**: `internal/riverbank/handlers.go`

```go
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
    path := chi.URLParam(r, "path")

    // 1. 查找 webhook path 对应的 agent binding
    // 2. 验证 webhook 签名（如果配置了）
    // 3. 解析 body 为 IncomingMessage
    // 4. 通过 pipeline 处理
    // 5. 返回响应
}
```

### 6. Cron 调度器

**新建文件**: `internal/lodge/drift/cron.go`

```go
type CronScheduler struct {
    pool      *pgxpool.Pool
    scheduler *asynq.Scheduler
}

func NewCronScheduler(redisAddr string, pool *pgxpool.Pool) (*CronScheduler, error) {
    loc, _ := time.LoadLocation("UTC")
    s := asynq.NewScheduler(
        asynq.RedisClientOpt{Addr: redisAddr},
        &asynq.SchedulerOpts{Location: loc},
    )
    return &CronScheduler{pool: pool, scheduler: s}, nil
}

func (c *CronScheduler) LoadAndSchedule(ctx context.Context) error {
    // 1. 从 cron_jobs 表加载所有 enabled 的 cron
    rows, _ := c.pool.Query(ctx, `
        SELECT id, agent_id, schedule, prompt
        FROM cron_jobs WHERE enabled = true
    `)

    // 2. 为每个 cron 注册 Asynq periodic task
    for _, job := range jobs {
        payload, _ := json.Marshal(map[string]string{
            "cron_job_id": job.ID.String(),
            "agent_id":    job.AgentID.String(),
            "prompt":      job.Prompt,
        })
        task := asynq.NewTask(TypeCronExecute, payload)
        c.scheduler.Register(job.Schedule, task)
    }

    return c.scheduler.Start()
}
```

**Cron 执行 Handler**（在 `drift/worker.go` 中）：

```go
func (w *Worker) handleCronExecute(ctx context.Context, task *asynq.Task) error {
    var payload struct {
        CronJobID string `json:"cron_job_id"`
        AgentID   string `json:"agent_id"`
        Prompt    string `json:"prompt"`
    }
    json.Unmarshal(task.Payload(), &payload)

    // 1. 创建新 session（session_key = "cron:<job_id>:<timestamp>"）
    // 2. 构造 IncomingMessage with prompt
    // 3. 通过 pipeline 执行
    // 4. 更新 cron_jobs.last_run_at, next_run_at, run_count
}
```

### 7. Temporal 工作流

**新建文件**: `internal/lodge/drift/temporal.go`

```go
import (
    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"
    "go.temporal.io/sdk/workflow"
)

type TemporalClient struct {
    client client.Client
}

func NewTemporalClient(host, namespace string) (*TemporalClient, error) {
    c, err := client.Dial(client.Options{
        HostPort:  host,
        Namespace: namespace,
    })
    return &TemporalClient{client: c}, err
}

// AgentTaskWorkflow 持久化的 agent 任务工作流
func AgentTaskWorkflow(ctx workflow.Context, input AgentTaskInput) (string, error) {
    // Activity 1: 准备上下文
    var prepResult PrepResult
    workflow.ExecuteActivity(ctx, PrepareContextActivity, input).Get(ctx, &prepResult)

    // Activity 2: 调用 LLM
    var llmResult LLMResult
    workflow.ExecuteActivity(ctx, InvokeLLMActivity, prepResult).Get(ctx, &llmResult)

    // Activity 3: 如果有工具调用，执行工具
    if len(llmResult.ToolCalls) > 0 {
        var toolResults []ToolResult
        workflow.ExecuteActivity(ctx, ExecuteToolsActivity, llmResult.ToolCalls).Get(ctx, &toolResults)
        // 循环直到无更多工具调用
    }

    // Activity 4: 持久化结果
    workflow.ExecuteActivity(ctx, PersistResultActivity, llmResult).Get(ctx, nil)

    return llmResult.Response, nil
}

// StartWorker 启动 Temporal worker
func (t *TemporalClient) StartWorker(taskQueue string) error {
    w := worker.New(t.client, taskQueue, worker.Options{})
    w.RegisterWorkflow(AgentTaskWorkflow)
    w.RegisterActivity(PrepareContextActivity)
    w.RegisterActivity(InvokeLLMActivity)
    w.RegisterActivity(ExecuteToolsActivity)
    w.RegisterActivity(PersistResultActivity)
    return w.Start()
}
```

---

## 关键文件清单

| 文件 | 操作 |
|------|------|
| `internal/riverbank/adapters/telegram/telegram.go` | **新建** |
| `internal/riverbank/adapters/discord/discord.go` | **新建** |
| `internal/riverbank/adapters/slack/slack.go` | **新建** |
| `internal/riverbank/adapters/manager.go` | **新建** — 适配器管理 |
| `internal/riverbank/handlers.go` | **实现** — handleWebhook |
| `internal/lodge/drift/cron.go` | **新建** — Cron 调度 |
| `internal/lodge/drift/worker.go` | **增强** — Cron 执行 handler |
| `internal/lodge/drift/temporal.go` | **新建** — Temporal 集成 |

## 可复用的已有代码

- `adapters.ChannelAdapter` 接口 — 统一的 Start/Stop/SendMessage（`adapter.go`）
- `adapters.IncomingMessage/OutgoingMessage` — 标准消息格式（`adapter.go`）
- `drift.Scheduler` — Asynq 客户端（`scheduler.go`）
- `drift.TypeCronExecute` — 任务类型常量（`scheduler.go:17`）
- DB schema: `channel_connections`, `agent_bindings`, `cron_jobs`

## 验收标准

1. **Telegram**: Telegram 消息 → Agent 响应出现在 Telegram 聊天中
2. **Discord**: Discord 消息 → Agent 在频道回复
3. **Slack**: Slack 消息 → Agent 在 thread 中回复
4. **Webhook**: `POST /hooks/my-webhook` → Agent 处理并返回结果
5. **Cron**: cron_jobs 表中配置的任务按 schedule 触发执行
6. **Temporal**: 长期工作流在 worker 重启后恢复执行

## 验证方法

```bash
# Telegram 测试
# 1. 创建 Telegram Bot，获取 token
# 2. 配置 capyclaw.yaml whiskers.telegram.bot_token
# 3. 在 agent_bindings 表绑定 chat_id → agent_id
# 4. 在 Telegram 中给 bot 发消息

# Cron 测试
psql -c "INSERT INTO cron_jobs (tenant_id, agent_id, name, schedule, prompt)
         VALUES ('...', '...', 'daily-report', '0 9 * * *', 'Generate daily report')"

# Temporal 测试（需要 Temporal server 运行）
# 查看 http://localhost:8233 Temporal UI
```

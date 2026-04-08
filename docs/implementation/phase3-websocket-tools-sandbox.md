# Phase 3: WebSocket 协议、工具执行、WASM 沙箱

## 目标

完成 WebSocket JSON-RPC 协议，实现实时客户端双向通信；实现工具调用循环（LLM 请求工具 → 执行 → 结果回送 → 继续对话）；交付 Tier 1 WASM 沙箱（Wazero），使 Agent 能安全执行工具。

## 前置条件

- Phase 2 完成（Agent CRUD、LLM Provider、Pipeline 核心阶段）

## 当前状态

### 已实现
- WebSocket 协议定义（`internal/riverbank/ws/protocol.go`）— 帧类型、方法名、事件名 100%
- WebSocket Hub（`internal/riverbank/ws/hub.go`）— 客户端注册/广播/单播 100%
- WebSocket 握手（`internal/riverbank/ws/handshake.go`）— 参数/结果结构体 100%
- WebSocket Handler 基础（`internal/riverbank/ws/handler.go`）— 升级连接、readPump/writePump goroutines ~70%
- 工具策略评估（`internal/burrow/nibble/policy.go`）— 级联策略 100%
- 工具注册表（`internal/burrow/nibble/registry.go`）— CRUD 100%
- 工具执行器框架（`internal/burrow/nibble/executor.go`）— 超时控制 ~40%，实际执行为 TODO
- 沙箱接口+选择逻辑（`internal/burrow/mudbath/sandbox.go`）— 接口定义 + SelectTier() 100%
- WASM 沙箱空壳（`internal/burrow/mudbath/wasm.go`）— 结构体定义，Execute 返回空

### 需要实现
- WebSocket readPump 中 JSON-RPC 帧解析和方法路由
- handleWebSocket 升级连接 + 认证
- Pipeline ToolPolicyFilter 和 ToolExecutor 阶段
- WASM 沙箱 Wazero 运行时初始化和实际执行
- seccomp BPF 配置

---

## 实施任务

### 1. WebSocket 帧解析和方法路由

**修改文件**: `internal/riverbank/ws/handler.go`

当前 `readPump` 读取消息但不解析。需要：

```go
func (c *Client) readPump(handler MessageHandler) {
    defer func() {
        c.hub.Unregister(c)
        c.conn.CloseNow()
    }()

    for {
        _, data, err := c.conn.Read(c.ctx)
        if err != nil {
            return
        }

        // 解析 JSON-RPC 2.0 帧
        var frame protocol.Request
        if err := json.Unmarshal(data, &frame); err != nil {
            c.sendError(frame.ID, protocol.ErrParseFailed, "invalid JSON-RPC frame")
            continue
        }

        // 方法路由
        switch frame.Method {
        case protocol.MethodChatSend:
            go handler.HandleChatSend(c, &frame)
        case protocol.MethodSessionGet:
            go handler.HandleSessionGet(c, &frame)
        case protocol.MethodAgentList:
            go handler.HandleAgentList(c, &frame)
        case "ping":
            c.sendResult(frame.ID, map[string]string{"pong": "ok"})
        default:
            c.sendError(frame.ID, protocol.ErrMethodNotFound, "unknown method")
        }
    }
}
```

**新增 MessageHandler 接口**：

```go
type MessageHandler interface {
    HandleChatSend(client *Client, req *protocol.Request)
    HandleSessionGet(client *Client, req *protocol.Request)
    HandleAgentList(client *Client, req *protocol.Request)
}
```

### 2. WebSocket ChatSend 处理

**新建或修改**: `internal/riverbank/ws/chat_handler.go`

```go
func (h *WSHandler) HandleChatSend(client *Client, req *protocol.Request) {
    // 1. 解析 params: {agent_id, session_key, content}
    // 2. 构造 PipelineRequest
    // 3. 调用 pipeline.Execute(ctx, pipelineReq)
    // 4. 从 channel 读取 chunks，发送为 JSON-RPC event 帧
    for chunk := range outputCh {
        event := protocol.Event{
            Event: protocol.EventMessageChunk,
            Data:  chunk,
        }
        client.send <- marshal(event)
    }
    // 5. 发送 done event
}
```

### 3. WebSocket Gateway 集成

**修改文件**: `internal/riverbank/handlers.go`

```go
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    // 1. 从 query param 获取 token: ?token=xxx
    token := r.URL.Query().Get("token")
    if token == "" {
        http.Error(w, "missing token", http.StatusUnauthorized)
        return
    }

    // 2. 验证 JWT
    claims, err := middleware.ValidateJWT(token, s.cfg.Riverbank.Auth.JWT)

    // 3. 升级 WebSocket 连接
    conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
        OriginPatterns: s.cfg.Riverbank.WebSocket.AllowedOrigins,
    })

    // 4. 创建 Client，设置 tenant context
    client := ws.NewClient(conn, claims.UserID, claims.TenantID)
    s.wsHub.Register(client)

    // 5. 创建 handler 并启动 pump
    handler := ws.NewWSHandler(s.pipeline, s.agentRepo, s.sessionRepo)
    go client.WritePump()
    client.ReadPump(handler)
}
```

### 4. Pipeline ToolPolicyFilter 实现

**修改文件**: `internal/burrow/pipeline.go`

```go
type ToolPolicyFilter struct {
    registry *nibble.Registry
}

func (s *ToolPolicyFilter) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
    allTools := s.registry.List()
    var allowedTools []ToolDef

    // 解析 agent 的 tool_policy
    agentPolicy := nibble.ToolPolicy{
        DefaultAction: nibble.PolicyAction(req.AgentConfig.ToolPolicy["default"]),
    }

    for _, tool := range allTools {
        action := nibble.EvaluatePolicy(tool.Name, agentPolicy)
        if action != nibble.PolicyDeny {
            allowedTools = append(allowedTools, *tool)
        }
    }

    // 将 allowedTools 转为 llm.Tool 格式，注入 PipelineRequest
    req.AvailableTools = convertToLLMTools(allowedTools)
    return req, nil
}
```

需要在 `PipelineRequest` 中增加 `AvailableTools []llm.Tool` 字段。

### 5. Pipeline ToolExecutor 实现

**修改文件**: `internal/burrow/pipeline.go`

工具执行循环是 Agent 的核心特性：

```go
type ToolExecutor struct {
    executor   *nibble.Executor
    llmClient  llm.Client
    maxIter    int  // 最大工具调用轮次（防止无限循环）
}

func (s *ToolExecutor) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
    // LLMInvoker 阶段已完成首次 LLM 调用
    // 检查响应中是否包含 tool_call
    if len(req.ToolCalls) == 0 {
        return req, nil // 无工具调用，直接跳过
    }

    for iteration := 0; iteration < s.maxIter; iteration++ {
        // 1. 逐个执行工具调用
        var results []ToolResult
        for _, tc := range req.ToolCalls {
            toolDef, ok := s.executor.Registry().Get(tc.Name)
            if !ok {
                results = append(results, ToolResult{Error: "tool not found"})
                continue
            }
            tier := mudbath.SelectTier(toolDef.Source, toolDef.Source == "bundled")
            result, err := s.executor.Execute(ctx, &tc, tier)
            results = append(results, *result)
        }

        // 2. 将工具结果作为新消息发送给 LLM
        req.Messages = append(req.Messages, toolResultsToMessages(results)...)

        // 3. 再次调用 LLM
        chunks, err := s.llmClient.Stream(ctx, buildLLMRequest(req))
        // 收集响应，检查是否还有 tool_call

        // 4. 如果无更多 tool_call，退出循环
        if len(newToolCalls) == 0 {
            break
        }
        req.ToolCalls = newToolCalls
    }
    return req, nil
}
```

### 6. WASM 沙箱实现

**修改文件**: `internal/burrow/mudbath/wasm.go`

```go
import (
    "context"
    "github.com/tetratelabs/wazero"
    "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

type WASMSandbox struct {
    runtime wazero.Runtime
    config  WASMConfig
}

type WASMConfig struct {
    MaxMemoryPages   uint32 // 每页 64KB
    MaxExecDuration  time.Duration
    AllowedHostFuncs []string
}

func NewWASMSandbox(cfg WASMConfig) (*WASMSandbox, error) {
    ctx := context.Background()

    // 创建 Wazero 运行时
    runtimeConfig := wazero.NewRuntimeConfig().
        WithMemoryLimitPages(cfg.MaxMemoryPages)

    rt := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

    // 注册 WASI（标准 I/O，但无文件系统和网络）
    wasi_snapshot_preview1.MustInstantiate(ctx, rt)

    return &WASMSandbox{
        runtime: rt,
        config:  cfg,
    }, nil
}

func (s *WASMSandbox) Execute(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
    // 1. 设置执行超时
    ctx, cancel := context.WithTimeout(ctx, s.config.MaxExecDuration)
    defer cancel()

    // 2. 编译 WASM 模块
    compiled, err := s.runtime.CompileModule(ctx, wasmBytes)

    // 3. 配置模块：
    //    - 标准输出/错误捕获到 buffer
    //    - 无文件系统挂载
    //    - 环境变量注入
    modConfig := wazero.NewModuleConfig().
        WithStdout(&stdout).
        WithStderr(&stderr).
        WithArgs(req.Args...).
        WithName(req.Command)

    for k, v := range req.Env {
        modConfig = modConfig.WithEnv(k, v)
    }

    // 4. 实例化并运行
    mod, err := s.runtime.InstantiateModule(ctx, compiled, modConfig)

    // 5. 收集结果
    return &ExecResult{
        Stdout:   stdout.String(),
        Stderr:   stderr.String(),
        ExitCode: 0,
    }, nil
}

func (s *WASMSandbox) Close() error {
    return s.runtime.Close(context.Background())
}
```

### 7. 工具执行器与沙箱对接

**修改文件**: `internal/burrow/nibble/executor.go`

```go
func (e *Executor) Execute(ctx context.Context, call *ToolCall, tier SandboxTier) (*ToolResult, error) {
    start := time.Now()

    ctx, cancel := context.WithTimeout(ctx, e.timeout)
    defer cancel()

    // 获取或创建对应 tier 的沙箱
    sandbox, err := e.getSandbox(tier)
    if err != nil {
        return nil, err
    }

    // 构造执行请求
    execReq := &mudbath.ExecRequest{
        Command:    call.Name,
        Args:       []string{mustMarshal(call.Input)},
        TimeoutSec: int(e.timeout.Seconds()),
        MemoryMB:   256, // 默认，可从 config 读取
    }

    result, err := sandbox.Execute(ctx, execReq)
    if err != nil {
        return &ToolResult{
            CallID: call.ID,
            Error:  err.Error(),
            Duration: time.Since(start),
            SandboxTier: tier,
        }, nil
    }

    return &ToolResult{
        CallID:      call.ID,
        Output:      result.Stdout,
        Error:       result.Stderr,
        Duration:    time.Since(start),
        SandboxTier: tier,
    }, nil
}
```

### 8. seccomp 配置（可选，Linux only）

**新建文件**: `internal/burrow/nibble/seccomp.go`

```go
// SeccompProfile 定义 syscall 过滤规则
type SeccompProfile struct {
    Name         string
    AllowedCalls []string
}

var DefaultProfile = SeccompProfile{
    Name: "default",
    AllowedCalls: []string{
        "read", "write", "close", "fstat", "mmap",
        "mprotect", "munmap", "brk", "exit_group",
        // 禁止: execve, fork, socket, connect, bind
    },
}
```

注意：seccomp 仅在 Linux 上生效，macOS 开发环境跳过。

---

## 关键文件清单

| 文件 | 操作 |
|------|------|
| `internal/riverbank/ws/handler.go` | **实现** — 帧解析 + 方法路由 |
| `internal/riverbank/ws/chat_handler.go` | **新建** — WebSocket chat 处理 |
| `internal/riverbank/handlers.go` | **实现** — handleWebSocket |
| `internal/burrow/pipeline.go` | **实现** — ToolPolicyFilter + ToolExecutor |
| `internal/burrow/mudbath/wasm.go` | **实现** — Wazero 运行时 |
| `internal/burrow/nibble/executor.go` | **实现** — 沙箱对接 |
| `internal/burrow/nibble/seccomp.go` | **新建** — seccomp 配置 |

## 可复用的已有代码

- `ws.Hub` — 客户端管理、广播、单播（`hub.go`）
- `ws.Client` — readPump/writePump goroutine 模型（`handler.go`）
- `protocol.Request/Response/Event` — JSON-RPC 帧类型（`protocol.go`）
- `nibble.EvaluatePolicy()` — 级联策略评估（`policy.go`）
- `nibble.Registry` — 工具注册/查询（`registry.go`）
- `mudbath.SelectTier()` — 根据 source/verified 选择沙箱级别（`sandbox.go`）
- `mudbath.Sandbox` 接口 — Execute/Tier/Close（`sandbox.go`）

## 验收标准

1. **WebSocket 连接**:
   - `wscat -c ws://localhost:18789/ws?token=<jwt>` 成功连接
   - 发送 `{"jsonrpc":"2.0","id":"1","method":"ping"}` → 收到 pong 响应

2. **WebSocket 对话**:
   - 发送 `chat.send` method → 收到多个 `agent.message.chunk` event 帧 → 最终收到 done

3. **工具调用**:
   - Agent 配置了一个简单工具（如 calculator）
   - 用户问 "What is 123 * 456?" → Agent 调用 calculator → 返回结果

4. **工具策略**:
   - 设置 tool_policy `{"calculator": "deny"}` → 工具不出现在 LLM 工具列表中

5. **WASM 沙箱**:
   - WASM 模块执行成功返回结果
   - 超过超时限制的模块被强制终止
   - WASM 模块无法访问文件系统

## 验证方法

```bash
# WebSocket 连接测试
npm install -g wscat
wscat -c "ws://localhost:18789/ws?token=<jwt>"

# 在 wscat 中发送
> {"jsonrpc":"2.0","id":"1","method":"chat.send","params":{"agent_id":"<id>","content":"Hello"}}

# 观察流式响应事件
```

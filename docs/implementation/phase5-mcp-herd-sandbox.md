# Phase 5: MCP 集成、多 Agent 协作、高级沙箱

## 目标

使 Agent 能连接外部 MCP 服务器并使用其工具；实现多 Agent 协作（父 Agent 委派任务给子 Agent）；完成 gVisor (Tier 2) 和 Firecracker (Tier 3) 沙箱。

## 前置条件

- Phase 3 完成（工具执行 + WASM 沙箱）
- Phase 4 完成（技能系统）

## 当前状态

### 已实现
- `mcp.Client`（`pkg/mcp/client.go`）— JSON-RPC 2.0 客户端完整实现，含 initialize 握手、ListTools、CallTool、receiveLoop ✓
- `mcp.Transport` 接口 + `StdioTransport`（`pkg/mcp/transport.go`）— Stdio 传输已实现 ✓
- `mcp.ToolDefinition/ToolCall/ToolResult` 类型（`pkg/mcp/types.go`）✓
- `herd.Manager`（`internal/burrow/herd/manager.go`）— Spawn/Stop/List 生命周期管理 ~50%
- `mudbath.Sandbox` 接口 + `SelectTier()` + `NewSandbox()` 工厂（`sandbox.go`）✓

### 需要实现
- SSE Transport（用于远程 MCP 服务器）
- MCP 连接管理（per-agent 生命周期、自动重连、工具缓存）
- MCP 工具桥接（nibble.Executor 路由到 MCP）
- Herd Manager 真正创建 Agent 实例并执行
- Herd Router（消息路由）和 Workflow（编排模式）
- gVisor 沙箱实现
- Firecracker 沙箱实现

---

## 实施任务

### 1. SSE Transport

**新增实现到**: `pkg/mcp/transport.go`

```go
type SSETransport struct {
    url    string
    client *http.Client
    recv   chan []byte
    cancel context.CancelFunc
}

func NewSSETransport(ctx context.Context, url string) (*SSETransport, error) {
    ctx, cancel := context.WithCancel(ctx)
    t := &SSETransport{
        url:    url,
        client: &http.Client{},
        recv:   make(chan []byte, 64),
        cancel: cancel,
    }

    // 1. GET url 建立 SSE 连接
    // 2. 从 SSE 事件中提取 endpoint URL（用于发送请求）
    // 3. 启动 readLoop 从 SSE 流读取消息

    go t.readLoop(ctx, resp.Body)
    return t, nil
}

func (t *SSETransport) Send(msg []byte) error {
    // POST 到 MCP server 的 message endpoint
    resp, err := t.client.Post(t.messageURL, "application/json", bytes.NewReader(msg))
    return err
}
```

### 2. MCP 连接管理器

**新建文件**: `internal/burrow/mcp_manager.go`

```go
type MCPConnectionManager struct {
    mu          sync.RWMutex
    connections map[uuid.UUID][]*MCPConnection // keyed by agent_id
}

type MCPConnection struct {
    client   *mcp.Client
    serverID string
    tools    []mcp.ToolDefinition
    lastSeen time.Time
}

func (m *MCPConnectionManager) Connect(ctx context.Context, agentID uuid.UUID, config MCPServerConfig) error {
    // 1. 根据 config.Type 创建 transport
    //    "stdio" → 启动子进程, pipe stdin/stdout
    //    "sse"   → NewSSETransport(ctx, config.URL)

    // 2. NewClient 执行 MCP 初始化握手
    // 3. ListTools 获取可用工具
    // 4. 注册到 nibble.Registry（动态工具）
    // 5. 保存连接
}

func (m *MCPConnectionManager) Disconnect(agentID uuid.UUID, serverID string) error {
    // client.Close()
    // 从 registry 反注册工具
}

func (m *MCPConnectionManager) GetTools(agentID uuid.UUID) []mcp.ToolDefinition {
    // 返回该 agent 连接的所有 MCP 工具
}
```

### 3. MCP 工具桥接

**修改文件**: `internal/burrow/nibble/executor.go`

在 Execute 方法中增加 MCP 工具路由：

```go
func (e *Executor) Execute(ctx context.Context, call *ToolCall, tier SandboxTier) (*ToolResult, error) {
    toolDef, ok := e.registry.Get(call.Name)
    if !ok {
        return nil, fmt.Errorf("tool not found: %s", call.Name)
    }

    // MCP 工具走 MCP 客户端而非沙箱
    if toolDef.Source == "mcp" {
        return e.executeMCP(ctx, call, toolDef)
    }

    // 本地工具走沙箱
    return e.executeSandbox(ctx, call, tier)
}

func (e *Executor) executeMCP(ctx context.Context, call *ToolCall, def *ToolDef) (*ToolResult, error) {
    mcpCall := mcp.ToolCall{
        Name:      call.Name,
        Arguments: call.Input,
    }
    result, err := e.mcpManager.CallTool(ctx, def.MCPServerID, mcpCall)
    // 转换 mcp.ToolResult → nibble.ToolResult
}
```

### 4. 多 Agent 协作实现

**修改文件**: `internal/burrow/herd/manager.go`

当前 Spawn 只创建 AgentHandle 元数据，需要真正创建 Agent 实例：

```go
func (m *Manager) Spawn(ctx context.Context, parentID uuid.UUID, slug string, agentConfig burrow.AgentConfig, pipeline *burrow.Pipeline) (*AgentHandle, error) {
    id := uuid.New()
    ctx, cancel := context.WithCancel(ctx)

    // 创建真正的 Agent 实例
    agent := burrow.NewAgent(id, m.tenantID, agentConfig, pipeline)

    handle := &AgentHandle{
        ID:       id,
        ParentID: parentID,
        Slug:     slug,
        Status:   "running",
        Cancel:   cancel,
        Agent:    agent,
        ResultCh: make(chan *burrow.StreamChunk, 64),
    }
    m.agents[id] = handle
    return handle, nil
}

// SendMessage 向子 Agent 发送消息并获取结果
func (m *Manager) SendMessage(ctx context.Context, agentID uuid.UUID, msg *burrow.IncomingMessage) (<-chan *burrow.StreamChunk, error) {
    handle, ok := m.agents[agentID]
    if !ok {
        return nil, fmt.Errorf("agent %s not found", agentID)
    }
    return handle.Agent.ProcessMessage(ctx, msg)
}
```

### 5. Herd Router

**新建文件**: `internal/burrow/herd/router.go`

```go
type Router struct {
    manager *Manager
}

// Route 根据委派规则将消息路由到合适的子 Agent
func (r *Router) Route(ctx context.Context, parentID uuid.UUID, msg *burrow.IncomingMessage) (uuid.UUID, error) {
    // 1. 查看是否有匹配 slug 的已运行 Agent
    // 2. 没有则 spawn 新 Agent
    // 3. 返回目标 Agent ID
}

// Delegate 父 Agent 委派任务给子 Agent
func (r *Router) Delegate(ctx context.Context, parentID uuid.UUID, targetSlug string, task string) (<-chan *burrow.StreamChunk, error) {
    // 1. 路由找到或创建目标 Agent
    // 2. 发送任务消息
    // 3. 返回结果流
}
```

### 6. Herd Workflow

**新建文件**: `internal/burrow/herd/workflow.go`

```go
type WorkflowType string

const (
    WorkflowSequential WorkflowType = "sequential" // 顺序执行
    WorkflowParallel   WorkflowType = "parallel"   // 并行扇出/扇入
    WorkflowSupervisor WorkflowType = "supervisor"  // 监督者分配
)

type Workflow struct {
    Type   WorkflowType
    Steps  []WorkflowStep
}

type WorkflowStep struct {
    AgentSlug string
    Prompt    string
    DependsOn []int // 依赖的步骤索引
}

func (w *Workflow) Execute(ctx context.Context, manager *Manager, pipeline *burrow.Pipeline) ([]string, error) {
    switch w.Type {
    case WorkflowSequential:
        return w.executeSequential(ctx, manager, pipeline)
    case WorkflowParallel:
        return w.executeParallel(ctx, manager, pipeline)
    case WorkflowSupervisor:
        return w.executeSupervisor(ctx, manager, pipeline)
    }
}
```

### 7. gVisor 沙箱

**新建文件**: `internal/burrow/mudbath/gvisor.go`

```go
type GVisorSandbox struct {
    runtimeClass string
    maxMemoryMB  int
    maxCPUMilli  int
}

func NewGVisorSandbox(cfg config.GVisorSandboxConfig) (*GVisorSandbox, error) {
    // 检查 runsc 可用性
    if _, err := exec.LookPath("runsc"); err != nil {
        return nil, fmt.Errorf("gVisor runsc not found: %w", err)
    }
    return &GVisorSandbox{...}, nil
}

func (s *GVisorSandbox) Execute(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
    // 1. 创建临时 OCI bundle
    // 2. 配置 config.json:
    //    - 内存限制: req.MemoryMB
    //    - CPU 限制
    //    - 只读 rootfs + writable tmpfs
    //    - 无网络 namespace（或白名单网络）

    // 3. 执行: runsc run --rootless <container-id>
    cmd := exec.CommandContext(ctx, "runsc", "run",
        "--rootless",
        "--network=none",
        containerID,
    )

    // 4. 捕获 stdout/stderr
    // 5. 清理容器
}
```

### 8. Firecracker 沙箱

**新建文件**: `internal/burrow/mudbath/firecracker.go`

```go
import firecracker "github.com/firecracker-microvm/firecracker-go-sdk"

type FirecrackerSandbox struct {
    kernelPath  string
    rootFSPath  string
    maxMemoryMB int
    maxVCPUs    int
    bootTimeout time.Duration
}

func NewFirecrackerSandbox(cfg config.FirecrackerConfig) (*FirecrackerSandbox, error) {
    return &FirecrackerSandbox{
        kernelPath:  cfg.KernelPath,
        rootFSPath:  cfg.RootFSPath,
        maxMemoryMB: cfg.MaxMemoryMB,
        maxVCPUs:    cfg.MaxVCPUs,
        bootTimeout: parseDuration(cfg.BootTimeout),
    }, nil
}

func (s *FirecrackerSandbox) Execute(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
    // 1. 创建 Firecracker VM 配置
    vmCfg := firecracker.Config{
        SocketPath:      socketPath,
        KernelImagePath: s.kernelPath,
        Drives: []firecracker.Drive{
            {PathOnHost: &s.rootFSPath, IsRootDevice: firecracker.Bool(true)},
        },
        MachineCfg: firecracker.MachineCfg{
            VcpuCount:  firecracker.Int64(int64(s.maxVCPUs)),
            MemSizeMib: firecracker.Int64(int64(s.maxMemoryMB)),
        },
    }

    // 2. 启动 VM（带 boot timeout）
    machine, err := firecracker.NewMachine(ctx, vmCfg)
    machine.Start(ctx)

    // 3. 通过 vsock 或 serial 发送命令
    // 4. 收集输出
    // 5. 停止 VM
    machine.StopVMM()
}
```

注意：Firecracker 需要 Linux + KVM 支持，macOS 开发环境不可用。

---

## 关键文件清单

| 文件 | 操作 |
|------|------|
| `pkg/mcp/transport.go` | **增强** — 新增 SSETransport |
| `internal/burrow/mcp_manager.go` | **新建** — MCP 连接生命周期 |
| `internal/burrow/nibble/executor.go` | **增强** — MCP 工具路由 |
| `internal/burrow/herd/manager.go` | **实现** — 真实 Agent 创建 + SendMessage |
| `internal/burrow/herd/router.go` | **新建** — 消息路由 |
| `internal/burrow/herd/workflow.go` | **新建** — 编排模式 |
| `internal/burrow/mudbath/gvisor.go` | **新建** — gVisor 沙箱 |
| `internal/burrow/mudbath/firecracker.go` | **新建** — Firecracker 沙箱 |

## 可复用的已有代码

- `mcp.Client` — 完整的 JSON-RPC 2.0 MCP 客户端（`client.go`），直接复用
- `mcp.StdioTransport` — Stdio 传输已完整实现（`transport.go`）
- `mcp.ToolDefinition/ToolCall/ToolResult` — 类型定义（`types.go`）
- `herd.Manager.Spawn/Stop/List` — 生命周期元数据管理（`manager.go`）
- `mudbath.SelectTier()` — 信任级别到沙箱映射（`sandbox.go:49-58`）
- `mudbath.NewSandbox()` — 工厂函数（`sandbox.go:61-72`）
- `nibble.Registry` — 工具注册表，MCP 工具可动态注册（`registry.go`）

## 验收标准

1. **MCP 集成**:
   - Agent 连接本地 MCP 服务器（stdio），列出工具
   - 对话中 Agent 使用 MCP 工具，结果正确返回
   - MCP 连接断开后自动重连

2. **多 Agent 协作**:
   - 父 Agent 调用 `herd.spawn` 创建子 Agent
   - 父 Agent 委派任务给子 Agent，获取结果
   - 子 Agent 完成后可被 stop

3. **gVisor 沙箱** (Linux):
   - community 信任级别的技能在 gVisor 中执行
   - 内存/CPU 限制生效
   - 无网络访问

4. **Firecracker 沙箱** (Linux + KVM):
   - untrusted 技能在 microVM 中执行
   - VM 在配置的 boot_timeout 内启动
   - VM 执行完成后自动关闭

## 验证方法

```bash
# MCP 测试 - 使用 MCP 测试服务器
npx @modelcontextprotocol/server-everything  # 启动测试 MCP server

# 通过 API 连接 MCP
curl -X POST http://localhost:18789/api/v1/agents/<id>/mcp \
  -d '{"type":"stdio","command":"npx","args":["@modelcontextprotocol/server-everything"]}'

# 验证工具列表
curl http://localhost:18789/api/v1/agents/<id>/tools

# 多 Agent 测试 - 对话中触发 Agent 委派
# "请让你的搜索助手帮我查找..." → Agent 应 spawn 子 Agent
```

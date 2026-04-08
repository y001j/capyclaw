# Phase 8: 前端、CLI、部署、生产环境加固

## 目标

构建完整的 Next.js 管理仪表板；实现 `capy` CLI 工具的所有子命令；准备 Docker/Kubernetes 生产部署；完成集成测试、负载测试和安全加固。

## 前置条件

- Phase 1-7 全部完成

## 当前状态

### 已实现
- Next.js 15 项目骨架（`web/`）— layout.tsx + page.tsx
- Zustand stores 骨架（`web/src/stores/agent.ts`, `session.ts`）
- WebSocket hook 骨架（`web/src/hooks/useWebSocket.ts`, `useStreamingChat.ts`）
- API/WS 客户端骨架（`web/src/lib/api.ts`, `ws.ts`）
- 环境变量配置（`NEXT_PUBLIC_GATEWAY_WS_URL`, `NEXT_PUBLIC_GATEWAY_HTTP_URL`）
- Cobra CLI 框架（`cmd/capy/main.go`）— 8 个子命令组已注册，所有 RunE 为空
- Docker Compose（`deploy/docker/docker-compose.yml`）— 基础服务定义
- Dockerfile.gateway + Dockerfile.web 存在

### 需要实现
- 前端所有页面和组件
- CLI 所有子命令的实际实现
- Kubernetes 部署清单
- 集成测试套件
- 负载测试
- 安全加固审查

---

## 实施任务

### 1. 前端 — API 客户端

**修改文件**: `web/src/lib/api.ts`

```typescript
const BASE_URL = process.env.NEXT_PUBLIC_GATEWAY_HTTP_URL || 'http://localhost:18789'

export class CapyClawAPI {
  private token: string

  constructor(token: string) { this.token = token }

  private async fetch<T>(path: string, options?: RequestInit): Promise<T> {
    const res = await fetch(`${BASE_URL}${path}`, {
      ...options,
      headers: {
        'Authorization': `Bearer ${this.token}`,
        'Content-Type': 'application/json',
        ...options?.headers,
      },
    })
    if (!res.ok) throw new ApiError(res.status, await res.json())
    return res.json()
  }

  // Agent CRUD
  listAgents() { return this.fetch<Agent[]>('/api/v1/agents') }
  getAgent(id: string) { return this.fetch<Agent>(`/api/v1/agents/${id}`) }
  createAgent(data: CreateAgentReq) { return this.fetch<Agent>('/api/v1/agents', { method: 'POST', body: JSON.stringify(data) }) }
  updateAgent(id: string, data: Partial<Agent>) { return this.fetch<Agent>(`/api/v1/agents/${id}`, { method: 'PATCH', body: JSON.stringify(data) }) }
  deleteAgent(id: string) { return this.fetch<void>(`/api/v1/agents/${id}`, { method: 'DELETE' }) }

  // Sessions
  listSessions(agentId: string) { return this.fetch<Session[]>(`/api/v1/agents/${agentId}/sessions`) }
  getSession(id: string) { return this.fetch<Session>(`/api/v1/sessions/${id}`) }
  listMessages(sessionId: string) { return this.fetch<Message[]>(`/api/v1/sessions/${sessionId}/messages`) }

  // Skills
  listSkills() { return this.fetch<Skill[]>('/api/v1/skills') }
  installSkill(data: InstallSkillReq) { return this.fetch<Skill>('/api/v1/skills/install', { method: 'POST', body: JSON.stringify(data) }) }

  // Memories
  searchMemories(agentId: string, query: string) { return this.fetch<Memory[]>(`/api/v1/agents/${agentId}/memories/search`, { method: 'POST', body: JSON.stringify({ query }) }) }

  // Admin
  listTenants() { return this.fetch<Tenant[]>('/api/v1/admin/tenants') }
  getTenantUsage(id: string) { return this.fetch<UsageReport>(`/api/v1/admin/tenants/${id}/usage`) }
  queryAudit(params: AuditQueryParams) { return this.fetch<AuditEvent[]>(`/api/v1/admin/audit?${new URLSearchParams(params)}`) }

  // Chat (SSE streaming)
  async *streamChat(agentId: string, message: string, sessionKey?: string): AsyncGenerator<ChatChunk> {
    const res = await fetch(`${BASE_URL}/v1/chat/completions`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${this.token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({
        model: 'claude-sonnet-4-20250514',
        stream: true,
        agent_id: agentId,
        session_key: sessionKey,
        messages: [{ role: 'user', content: message }],
      }),
    })
    // Parse SSE stream
    const reader = res.body!.getReader()
    const decoder = new TextDecoder()
    // yield chunks...
  }
}
```

### 2. 前端 — WebSocket 客户端

**修改文件**: `web/src/lib/ws.ts`

```typescript
export class CapyClawWS {
  private ws: WebSocket | null = null
  private pending = new Map<string, { resolve: Function; reject: Function }>()
  private listeners = new Map<string, ((data: any) => void)[]>()
  private nextId = 0

  connect(token: string) {
    const url = `${process.env.NEXT_PUBLIC_GATEWAY_WS_URL}/ws?token=${token}`
    this.ws = new WebSocket(url)
    this.ws.onmessage = (evt) => this.handleMessage(JSON.parse(evt.data))
  }

  async send(method: string, params: any): Promise<any> {
    const id = String(++this.nextId)
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject })
      this.ws!.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }))
    })
  }

  on(event: string, callback: (data: any) => void) {
    if (!this.listeners.has(event)) this.listeners.set(event, [])
    this.listeners.get(event)!.push(callback)
  }

  private handleMessage(msg: any) {
    if (msg.id && this.pending.has(msg.id)) {
      this.pending.get(msg.id)!.resolve(msg.result)
      this.pending.delete(msg.id)
    } else if (msg.event) {
      this.listeners.get(msg.event)?.forEach(cb => cb(msg.data))
    }
  }
}
```

### 3. 前端 — 页面结构

**使用 Next.js App Router 创建以下页面**：

```
web/src/app/
├── layout.tsx              # 全局布局 + 导航侧边栏
├── page.tsx                # 仪表板首页（Agent 列表概览）
├── agents/
│   ├── page.tsx           # Agent 列表
│   ├── new/page.tsx       # 创建 Agent 表单
│   └── [id]/
│       ├── page.tsx       # Agent 详情 + 配置编辑
│       ├── chat/page.tsx  # 实时对话界面（WebSocket 流式）
│       ├── sessions/page.tsx  # Session 列表
│       └── memories/page.tsx  # 记忆浏览器
├── skills/
│   └── page.tsx           # 技能市场 + 安装管理
├── admin/
│   ├── page.tsx           # 管理面板（租户管理）
│   ├── usage/page.tsx     # 用量图表（Recharts）
│   └── audit/page.tsx     # 审计日志查看器
└── login/
    └── page.tsx           # 登录页（next-auth）
```

**关键组件**（`web/src/components/`）：

- `ChatView.tsx` — 对话气泡列表 + 流式渲染
- `AgentCard.tsx` — Agent 卡片展示
- `MemoryBrowser.tsx` — 记忆搜索 + 查看
- `UsageChart.tsx` — Recharts 用量折线图
- `AuditTable.tsx` — 审计日志表格 + 筛选

### 4. 前端 — Zustand Stores

**修改文件**: `web/src/stores/agent.ts`

```typescript
import { create } from 'zustand'

interface AgentStore {
  agents: Agent[]
  loading: boolean
  fetchAgents: () => Promise<void>
  createAgent: (data: CreateAgentReq) => Promise<Agent>
  deleteAgent: (id: string) => Promise<void>
}

export const useAgentStore = create<AgentStore>((set, get) => ({
  agents: [],
  loading: false,
  fetchAgents: async () => {
    set({ loading: true })
    const agents = await api.listAgents()
    set({ agents, loading: false })
  },
  // ...
}))
```

### 5. CLI 实现

**修改文件**: `cmd/capy/main.go`

需要为每个子命令实现实际逻辑。CLI 通过 HTTP API 与 Gateway 交互。

```go
// capy agent list
func newAgentCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "agent", Short: "Manage agents"}

    listCmd := &cobra.Command{
        Use: "list",
        RunE: func(cmd *cobra.Command, args []string) error {
            client := newAPIClient()
            agents, err := client.ListAgents()
            // 表格输出
            tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
            fmt.Fprintln(tw, "ID\tNAME\tMODEL\tSTATUS")
            for _, a := range agents {
                fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.ID, a.Name, a.Model, a.Status)
            }
            return tw.Flush()
        },
    }

    // capy agent chat <slug> — 交互式终端对话
    chatCmd := &cobra.Command{
        Use:  "chat",
        Args: cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            agentSlug := args[0]
            // 1. 建立 WebSocket 连接
            // 2. 终端 readline 循环
            // 3. 发送 chat.send，流式打印响应
            scanner := bufio.NewScanner(os.Stdin)
            for {
                fmt.Print("> ")
                if !scanner.Scan() { break }
                input := scanner.Text()
                // 发送到 WebSocket，流式打印响应到 stdout
            }
        },
    }

    cmd.AddCommand(listCmd, createCmd, deleteCmd, chatCmd)
    return cmd
}
```

**新建文件**: `cmd/capy/client.go` — HTTP API 客户端封装

```go
type APIClient struct {
    baseURL string
    token   string
    http    *http.Client
}

func newAPIClient() *APIClient {
    return &APIClient{
        baseURL: viper.GetString("gateway_url"), // 默认 http://localhost:18789
        token:   viper.GetString("token"),
        http:    &http.Client{Timeout: 30 * time.Second},
    }
}
```

### 6. Kubernetes 部署

**新建目录**: `deploy/k8s/`

```
deploy/k8s/
├── namespace.yaml
├── gateway-deployment.yaml
├── gateway-service.yaml
├── gateway-configmap.yaml
├── gateway-hpa.yaml           # 水平自动扩缩
├── web-deployment.yaml
├── web-service.yaml
├── ingress.yaml               # TLS 终止
├── network-policy.yaml        # Pod 间网络隔离
├── pdb.yaml                   # Pod 中断预算
└── secrets.yaml               # 外部 secret 引用
```

**关键清单**:

```yaml
# gateway-deployment.yaml
apiVersion: apps/v1
kind: Deployment
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: gateway
        image: capyclaw/gateway:latest
        ports:
        - containerPort: 18789
        resources:
          requests: { cpu: "500m", memory: "512Mi" }
          limits:   { cpu: "2000m", memory: "2Gi" }
        livenessProbe:
          httpGet: { path: /healthz, port: 18789 }
        readinessProbe:
          httpGet: { path: /readyz, port: 18789 }
        envFrom:
        - configMapRef: { name: gateway-config }

# gateway-hpa.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
spec:
  minReplicas: 2
  maxReplicas: 20
  metrics:
  - type: Resource
    resource: { name: cpu, target: { type: Utilization, averageUtilization: 70 } }
```

### 7. 集成测试

**修改/新建**: `tests/integration/`

```go
// gateway_test.go — 端到端测试
func TestAgentCRUD(t *testing.T) {
    // 1. 启动测试 Gateway（连接测试数据库）
    // 2. 创建 Agent → 查询 → 更新 → 删除
    // 3. 验证每步的 HTTP 状态码和响应体
}

func TestChatCompletions(t *testing.T) {
    // 1. 创建 Agent
    // 2. POST /v1/chat/completions with stream=true
    // 3. 验证 SSE 流包含文本响应
    // 4. 验证 messages 表有记录
}

func TestTenantIsolation(t *testing.T) {
    // 1. 以 Tenant A 创建 Agent
    // 2. 以 Tenant B 查询 → 应该看不到
    // 3. 以 Tenant B 直接访问 Agent ID → 应该 404
}

func TestRateLimiting(t *testing.T) {
    // 快速发送 60+ 请求 → 验证 429
}

func TestWebSocket(t *testing.T) {
    // 1. WebSocket 连接
    // 2. 发送 chat.send
    // 3. 验证收到流式 event 帧
}

func TestQuotaEnforcement(t *testing.T) {
    // 1. 设置极低的 monthly_token_budget
    // 2. 发送消息 → 应该被拒绝
}
```

### 8. 负载测试

**新建目录**: `tests/load/`

```javascript
// tests/load/websocket.js (k6 脚本)
import ws from 'k6/ws'
import { check } from 'k6'

export const options = {
  stages: [
    { duration: '30s', target: 100 },
    { duration: '1m', target: 500 },
    { duration: '1m', target: 1000 },
    { duration: '30s', target: 0 },
  ],
}

export default function () {
  const url = `ws://localhost:18789/ws?token=${__ENV.TOKEN}`
  const res = ws.connect(url, {}, function (socket) {
    socket.on('open', () => {
      socket.send(JSON.stringify({
        jsonrpc: '2.0', id: '1',
        method: 'chat.send',
        params: { agent_id: __ENV.AGENT_ID, content: 'Hello' },
      }))
    })
    socket.on('message', (data) => {
      const msg = JSON.parse(data)
      if (msg.event === 'agent.message.done') socket.close()
    })
    socket.setTimeout(() => socket.close(), 30000)
  })
  check(res, { 'status is 101': (r) => r && r.status === 101 })
}
```

### 9. 安全加固清单

1. **输入验证**: 所有 Handler 使用 `go-playground/validator` 验证请求体
2. **SQL 注入**: 确认所有查询使用参数化查询（`$1`, `$2`）
3. **TLS 强制**: 生产环境 `tls.enabled=true`, `tls.min_version=1.3`
4. **CORS**: 确认 AllowedOrigins 不含通配符 `*`
5. **CSP 头**: 前端设置 `Content-Security-Policy`
6. **速率限制调优**: 根据负载测试结果调整限流参数
7. **JWT 密钥安全**: 生产环境密钥通过 Vault 管理，不硬编码
8. **审计覆盖**: 确认所有关键操作有审计日志
9. **错误信息**: 生产环境不暴露堆栈和内部错误细节
10. **依赖审计**: `go mod tidy && govulncheck ./...`

---

## 关键文件清单

| 文件/目录 | 操作 |
|-----------|------|
| `web/src/lib/api.ts` | **实现** — 完整 API 客户端 |
| `web/src/lib/ws.ts` | **实现** — WebSocket 客户端 |
| `web/src/stores/agent.ts` | **实现** — Zustand store |
| `web/src/stores/session.ts` | **实现** — Zustand store |
| `web/src/app/agents/` | **新建** — Agent 页面 |
| `web/src/app/admin/` | **新建** — 管理页面 |
| `web/src/components/` | **新建** — UI 组件 |
| `cmd/capy/main.go` | **实现** — 所有子命令 |
| `cmd/capy/client.go` | **新建** — API 客户端 |
| `deploy/k8s/` | **新建** — K8s 清单 |
| `tests/integration/` | **实现** — 端到端测试 |
| `tests/load/` | **新建** — 负载测试 |

## 可复用的已有代码

- `web/src/hooks/useWebSocket.ts` — WebSocket hook 骨架
- `web/src/hooks/useStreamingChat.ts` — 流式聊天 hook 骨架
- Cobra CLI 框架 — 子命令结构已定义（`cmd/capy/main.go`）
- Docker Compose — 基础服务定义（`deploy/docker/docker-compose.yml`）
- Dockerfile.gateway + Dockerfile.web — 多阶段构建模板

## 验收标准

1. **前端**: 浏览器访问 `http://localhost:3000` → 登录 → 创建 Agent → 对话 → 查看记忆
2. **CLI**: `capy agent list` 列出 Agent；`capy agent chat my-agent` 交互式对话
3. **Docker**: `docker compose up` 一键启动完整栈
4. **K8s**: `kubectl apply -f deploy/k8s/` 部署到集群
5. **负载**: 1000 并发 WebSocket 连接稳定运行
6. **安全**: `govulncheck` 无高危漏洞；所有 CVE 缓解措施验证通过
7. **测试**: `make test-integration` 全部通过

## 验证方法

```bash
# 前端开发
cd web && npm run dev  # http://localhost:3000

# CLI 测试
go run cmd/capy/main.go agent list
go run cmd/capy/main.go agent chat test-agent

# Docker 全栈
cd deploy/docker && docker compose up -d
curl http://localhost:18789/healthz

# 集成测试
make test-integration

# 负载测试
k6 run tests/load/websocket.js --env TOKEN=<jwt> --env AGENT_ID=<id>

# 安全审计
govulncheck ./...
```

# CapyClaw

<p align="center">
  <strong>安全优先、水平可扩展的 AI Agent 平台</strong>
</p>

<p align="center">
  <a href="#快速开始">快速开始</a> |
  <a href="#架构">架构</a> |
  <a href="#开发">开发</a> |
  <a href="docs/capyclaw-framework-guide.md">框架指南</a> |
  <a href="docs/CapyClaw_Research.md">安全研究</a>
</p>

---

## 概述

CapyClaw 是对 OpenClaw 的全面 Go + Next.js 重新实现，旨在消除原始 Node.js 系统中的 13+ 个 CVE 以及架构局限性。它提供一个始终在线的个人 AI 助手，可连接 25+ 个消息平台，通过 LLM 执行自主任务，并支持多租户企业部署。

**核心设计原则：**

- **默认安全** — 全链路 mTLS/OIDC，无不安全的认证开关，密钥永不以明文落盘
- **无状态计算** — 任意实例可服务任意会话；所有状态存储在 PostgreSQL + Redis 中
- **每会话一协程** — 每会话 2-4 KB 栈空间，无事件循环阻塞
- **分层沙箱** — WASM (Wazero) → gVisor → Firecracker，按信任级别逐层升级
- **默认可观测** — 从第一天起内置 OpenTelemetry 追踪、指标和日志

> **状态：** v0.1.0-alpha — 已实现：核心脚手架、配置、数据库 schema、中间件、8 阶段 Agent 流水线、LLM 提供者（Anthropic/OpenAI）、MCP 集成（stdio + SSE）、多 Agent 协作（顺序/并行/监督者工作流）、渠道适配器（Telegram/Discord/Slack + Webhook）、分层沙箱（WASM/gVisor/Firecracker）、Cron 定时调度和 Temporal 持久化工作流。Google/Ollama 提供者、CapyHub 技能市场和前端 UI 正在开发中。

## 功能特性

- **多 LLM 支持** — Anthropic (Claude)、OpenAI、Google (Gemini)、Ollama（本地模型）
- **8 阶段 Agent 流水线** — SessionResolver → WorkspaceLoader → ModelSelector → PromptBuilder → ToolPolicyFilter → LLMInvoker → ToolExecutor → SessionPersister
- **三层记忆系统** — 情景记忆、语义记忆和程序性记忆，配合 pgvector 混合搜索（向量 + 全文检索）
- **渠道适配器** — Telegram、Discord、Slack、WhatsApp、WeChat、Signal、Matrix
- **OpenAI 兼容 API** — `POST /v1/chat/completions` 即插即用兼容
- **MCP 协议支持** — Model Context Protocol 客户端，用于工具集成（stdio + SSE 传输）
- **多 Agent 协作** — Agent 派生、Agent 间消息路由、Temporal 持久化工作流（顺序/并行/监督者模式）
- **企业级多租户** — 行级安全、租户级速率限制、基于 Casbin 的 RBAC/ABAC
- **异步上下文压缩** — 80% 令牌阈值时后台摘要，告别 30 秒冻结

## 架构

所有内部包采用**水豚生态**主题命名：

| 代号 | 路径 | 职责 |
|------|------|------|
| **Riverbank**（河岸） | `internal/riverbank/` | HTTP/WebSocket 网关、路由、中间件、渠道适配器 |
| **Burrow**（洞穴） | `internal/burrow/` | Agent 运行时、LLM 提供者、沙箱、工具执行、多 Agent 协作 |
| **Pond**（池塘） | `internal/pond/` | PostgreSQL + pgvector 存储、迁移、向量搜索 (Ripple) |
| **Lodge**（栖所） | `internal/lodge/` | Redis 发布/订阅 + 在线状态 (Tide)、Temporal 工作流 + 定时任务 (Drift) |
| **Wetland**（湿地） | `internal/wetland/` | 日志 + 审计 (Footprint)、多租户 + RBAC (Marsh)、Vault 密钥 (Canopy)、OpenTelemetry (Vapor) |

### 请求流程

```
客户端 → 负载均衡 → Riverbank (chi, 端口 18789)
          │
          ├─ 全局中间件：RealIP → RequestID → Recoverer → CORS
          ├─ 认证中间件：Auth → Tenant → RateLimit
          │
          └─ Burrow 8 阶段流水线
               SessionResolver → WorkspaceLoader → ModelSelector → PromptBuilder
               → ToolPolicyFilter → LLMInvoker → ToolExecutor → SessionPersister
```

### 沙箱层级 (Mudbath)

| 层级 | 技术 | 适用场景 | 延迟 |
|------|------|----------|------|
| 1 | WASM (Wazero) | 插件/技能（默认） | 微秒级启动 |
| 2 | gVisor (runsc) | 用户工具，中等风险 | 20-50% I/O 开销 |
| 3 | Firecracker 微虚拟机 | 不受信任的代码执行 | ~125ms 冷启动 |

## 技术栈

**后端：** Go 1.25, chi, pgx v5, go-redis v9, Temporal, Wazero, OpenTelemetry, Cobra, Viper

**前端：** Next.js 15, React 19, TypeScript, Zustand, shadcn/ui, Tailwind CSS, next-auth 5

**基础设施：** PostgreSQL 16 + pgvector, Redis 7.4, Temporal 1.25, HashiCorp Vault, Grafana + Tempo + Loki

## 快速开始

### 前置要求

- Go 1.25+
- Node.js 20+
- Docker & Docker Compose
- Make

### 安装

```bash
# 克隆仓库
git clone https://github.com/your-org/capyclaw.git
cd capyclaw

# 复制并编辑配置文件
cp capyclaw.example.yaml capyclaw.yaml

# 一键初始化：启动基础设施、运行迁移、填充种子数据
./scripts/setup-dev.sh
```

### 开发

```bash
# 启动后端（Docker 基础设施 + 热重载）
make dev

# 启动前端（另开终端）
cd web && npm run dev
```

网关监听 **18789** 端口，前端开发服务器监听 **3000** 端口。

### 构建

```bash
make build        # 构建网关二进制文件 + capy CLI 到 bin/
make clean        # 清除构建产物
```

## 配置

CapyClaw 使用分层 YAML 配置，支持环境变量覆盖：

```bash
# 格式：CAPYCLAW_<区块>_<键名>
CAPYCLAW_RIVERBANK_PORT=18789
CAPYCLAW_LOG_LEVEL=debug
```

所有可用选项参见 [`capyclaw.example.yaml`](capyclaw.example.yaml)。关键默认值：

| 配置项 | 默认值 |
|--------|--------|
| 网关端口 | 18789 |
| 默认 LLM | `claude-sonnet-4-20250514` |
| 上下文窗口 | 200k tokens |
| 压缩阈值 | 80% |
| pgvector 维度 | 1536 (HNSW, m=16, ef=200) |
| TLS 最低版本 | 1.3 |

## 测试

```bash
make test              # 全部测试（120s 超时）
make test-unit         # 仅单元测试（60s 超时）
make test-integration  # 集成测试（300s，需要运行中的基础设施）
```

集成测试位于 `tests/integration/`，需要 PostgreSQL 和 Redis 处于运行状态。

## 代码质量

```bash
make lint    # golangci-lint
make fmt     # gofmt + goimports
make vet     # go vet
```

## 数据库

```bash
make migrate       # 执行迁移
make migrate-down  # 回滚迁移
make seed          # 填充测试数据（默认租户 + 管理员用户，幂等操作）
```

使用 PostgreSQL 16 + pgvector。核心表包括：`tenants`、`users`、`devices`、`agents`、`sessions`、`messages`、`agent_memories`（含向量嵌入）、`audit_logs`、`quotas`、`billing`。通过每次连接 `SET app.tenant_id` 强制行级安全。

## 安全

CapyClaw 的诞生源于 OpenClaw 中发现的 13+ 个 CVE。安全是从根基构建的，而非事后附加：

- **无明文凭据** — 所有密钥通过 HashiCorp Vault 管理
- **无 shell 扩展** — 工具执行使用 `exec()` 配合 seccomp-BPF 配置
- **仅追加审计日志** — `audit_logs` 表条目不可修改
- **租户隔离** — 始终传播租户上下文；跨租户查询不可能发生
- **强制 TLS** — 生产环境所有外部连接使用 TLS 1.3+
- **MCP 服务器信任** — 所有 MCP 服务器视为不受信任；工具描述经过提示注入净化
- **Origin 校验** — WebSocket 连接强制 Origin 头校验（防止 ClawJacked 式攻击）

详细的威胁分析、CVE 分解和架构设计理由参见 [`docs/CapyClaw_Research.md`](docs/CapyClaw_Research.md)。

## 项目结构

```
capyclaw/
├── cmd/
│   ├── capy/                   # CLI 命令行工具
│   └── gateway/                # 网关服务器
├── internal/
│   ├── riverbank/              # 网关：HTTP/WS 服务器、中间件、渠道适配器
│   ├── burrow/                 # Agent：运行时、LLM、沙箱、工具、多 Agent 协作
│   ├── pond/                   # 存储：PostgreSQL、pgvector、迁移
│   ├── lodge/                  # 基础设施：Redis 发布/订阅、Temporal 工作流
│   ├── wetland/                # 平台：日志、RBAC、Vault、OpenTelemetry
│   └── shared/                 # 共享配置、错误、类型
├── pkg/
│   ├── mcp/                    # Model Context Protocol 客户端
│   ├── protocol/               # 自定义线路协议 (JSON-RPC)
│   └── skills/                 # 技能生命周期管理
├── web/                        # Next.js 15 前端
├── deploy/                     # Docker Compose + Dockerfile
├── tests/integration/          # 集成测试
├── scripts/                    # 开发脚本（初始化、迁移、填充）
├── docs/                       # 文档
├── capyclaw.example.yaml       # 配置模板
└── Makefile                    # 构建、测试、lint 命令
```

## 文档

- [`docs/capyclaw-framework-guide.md`](docs/capyclaw-framework-guide.md) — 详细实现指南、API 端点、设计决策
- [`docs/CapyClaw_Research.md`](docs/CapyClaw_Research.md) — 威胁分析、CVE 分解、架构设计理由

## 许可证

待定

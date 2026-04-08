# CapyClaw Framework Guide

> Version: 0.1.0-alpha | Last Updated: 2026-03-21
> A security-first, horizontally-scalable AI agent platform built with Go + React/Next.js

---

## 1. Project overview

CapyClaw is a ground-up reimplementation of OpenClaw, designed to eliminate the structural security, reliability, and scalability weaknesses of the original Node.js/file-based architecture. The system provides a personal, always-on AI assistant that connects to 25+ messaging platforms, executes autonomous tasks via LLMs, and supports multi-tenant enterprise deployment.

### 1.1 Design principles

- **Security by default**: No insecure auth toggles. mTLS/OIDC everywhere. Secrets never touch disk in plaintext.
- **Stateless compute**: Any CapyClaw instance can serve any session. All state lives in PostgreSQL + Redis.
- **Goroutine-per-session**: Each agent session runs in its own goroutine with 2-4 KB stack overhead.
- **Tiered sandboxing**: WASM (Wazero) → gVisor → Firecracker, escalating by trust level.
- **Observable by default**: OpenTelemetry traces, metrics, and logs from day one.
- **API-compatible where possible**: Preserve OpenClaw's WebSocket JSON-RPC protocol and OpenAI-compatible HTTP endpoints for ecosystem compatibility.

### 1.2 Naming convention — capybara ecology theme

All components are named after capybara habitat and behavior traits. This is not just whimsy — the names create a shared mental model for the team.

| Layer | Codename | Metaphor | Technical Role |
|-------|----------|----------|----------------|
| Frontend | **Meadow** | Capybara's grazing field | React/Next.js control dashboard |
| Gateway | **Riverbank** | Water-land boundary | Go HTTP/WebSocket API server |
| Agent Core | **Burrow** | Underground nest | Agent runtime + orchestration |
| Memory/Storage | **Pond** | Capybara's favorite pool | PostgreSQL + pgvector |
| Infrastructure | **Lodge** | Shelter structure | Redis + Temporal |
| Platform Services | **Wetland** | Ecosystem support layer | Observability, audit, tenancy |
| CLI | **capy** | The animal itself | Command-line interface |
| Config | **capyclaw.yaml** | — | Main configuration file |
| Plugin Market | **CapyHub** | Social gathering | Verified skill marketplace |

---

## 2. Technology stack

### 2.1 Backend (Go 1.23+)

| Component | Library | Version |
|-----------|---------|---------|
| HTTP router | `github.com/go-chi/chi/v5` | v5.1+ |
| WebSocket | `github.com/coder/websocket` | v1.8+ |
| PostgreSQL driver | `github.com/jackc/pgx/v5` | v5.7+ |
| Vector search | `github.com/pgvector/pgvector-go` | v0.3+ |
| Redis | `github.com/redis/go-redis/v9` | v9.7+ |
| JWT | `github.com/golang-jwt/jwt/v5` | v5.2+ |
| OIDC | `github.com/coreos/go-oidc/v3` | v3.11+ |
| RBAC | `github.com/casbin/casbin/v2` | v2.100+ |
| Task queue | `github.com/hibiken/asynq` | v0.25+ |
| Durable workflows | `go.temporal.io/sdk` | v1.29+ |
| WASM sandbox | `github.com/tetratelabs/wazero` | v1.8+ |
| Observability | `go.opentelemetry.io/otel` | v1.32+ |
| Circuit breaker | `github.com/sony/gobreaker/v2` | v2.0+ |
| Validation | `github.com/go-playground/validator/v10` | v10.23+ |
| CLI framework | `github.com/spf13/cobra` | v1.8+ |
| Config management | `github.com/spf13/viper` | v1.19+ |
| LLM abstraction | `github.com/tmc/langchaingo` | v0.1.13+ |
| MCP protocol | `github.com/modelcontextprotocol/go-sdk` | latest |
| Browser automation | `github.com/chromedp/chromedp` | v0.11+ |
| Structured logging | `log/slog` (stdlib) | Go 1.23 |
| Secret management | `github.com/hashicorp/vault/api` | v1.15+ |
| Rate limiting | `golang.org/x/time/rate` + `github.com/ulule/limiter/v3` | latest |

### 2.2 Frontend (React/Next.js)

| Component | Library | Version |
|-----------|---------|---------|
| Framework | Next.js (App Router) | 15+ |
| Language | TypeScript | 5.6+ |
| State management | Zustand | 5+ |
| WebSocket client | Native WebSocket API | — |
| UI components | shadcn/ui + Tailwind CSS | latest |
| Charts | Recharts | 2.13+ |
| Forms | React Hook Form + Zod | latest |
| Auth | next-auth (OIDC provider) | 5+ |

### 2.3 Infrastructure

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Database | PostgreSQL 16 + pgvector 0.8 | Primary data store + vector search |
| Cache / Pub-Sub | Redis 7.4 (Cluster mode) | Session state, events, rate limiting |
| Task orchestration | Temporal Server 1.25 | Durable multi-agent workflows |
| Container runtime | Docker + gVisor (runsc) | Tier 2 tool sandboxing |
| MicroVM | Firecracker | Tier 3 untrusted code isolation |
| Secrets | HashiCorp Vault | Dynamic secret management |
| Observability | Grafana + Tempo + Loki | Traces, metrics, logs |
| Load balancer | Caddy / Traefik | L7, TLS termination, WS affinity |
| Container orchestration | Kubernetes 1.31+ | Production deployment |

---

## 3. Project directory structure

```
capyclaw/
├── cmd/                          # Executable entry points
│   ├── capy/                     # CLI binary
│   │   └── main.go
│   └── gateway/                  # Gateway server binary
│       └── main.go
│
├── internal/                     # Private application code (not importable externally)
│   ├── riverbank/                # Gateway layer
│   │   ├── server.go             # HTTP + WebSocket server setup
│   │   ├── router.go             # Chi router configuration
│   │   ├── middleware/           
│   │   │   ├── auth.go           # Sentry: mTLS + OIDC + JWT validation
│   │   │   ├── ratelimit.go      # Rapids: per-tenant rate limiting
│   │   │   ├── cors.go           # Origin validation (prevents ClawJacked)
│   │   │   ├── tenant.go         # Tenant context injection
│   │   │   └── requestid.go      # Request ID propagation
│   │   ├── ws/                   
│   │   │   ├── handler.go        # WebSocket upgrade + connection mgmt
│   │   │   ├── protocol.go       # JSON-RPC frame types (req/res/event)
│   │   │   ├── handshake.go      # Challenge-response device pairing
│   │   │   └── hub.go            # Connection registry + broadcast
│   │   ├── http/                 
│   │   │   ├── chat.go           # POST /v1/chat/completions (OpenAI-compat)
│   │   │   ├── responses.go      # POST /v1/responses (OpenResponses)
│   │   │   ├── tools.go          # POST /tools/invoke
│   │   │   ├── hooks.go          # POST /hooks/{path} webhook ingress
│   │   │   └── health.go         # /healthz + /readyz
│   │   └── adapters/             # Whiskers: channel adapters
│   │       ├── adapter.go        # Common ChannelAdapter interface
│   │       ├── telegram/
│   │       ├── discord/
│   │       ├── slack/
│   │       ├── whatsapp/
│   │       ├── wechat/
│   │       ├── signal/
│   │       └── matrix/
│   │
│   ├── burrow/                   # Agent core layer
│   │   ├── agent.go              # Agent struct + lifecycle
│   │   ├── session.go            # Session management + goroutine-per-session
│   │   ├── pipeline.go           # 8-stage execution pipeline orchestrator
│   │   ├── instinct/             # Instinct: prompt engineering
│   │   │   ├── builder.go        # System prompt assembly
│   │   │   ├── context.go        # Context window management
│   │   │   ├── compaction.go     # Async background compaction
│   │   │   └── templates.go      # Prompt templates
│   │   ├── nibble/               # Nibble: tool execution
│   │   │   ├── executor.go       # Tool dispatch + result collection
│   │   │   ├── policy.go         # Cascading tool policy filter
│   │   │   ├── registry.go       # Tool registry + capability declarations
│   │   │   └── seccomp.go        # Seccomp-BPF profiles for safe binaries
│   │   ├── mudbath/              # Mudbath: sandboxing
│   │   │   ├── wasm.go           # Tier 1: Wazero WASM runtime
│   │   │   ├── gvisor.go         # Tier 2: gVisor container management
│   │   │   ├── firecracker.go    # Tier 3: Firecracker microVM
│   │   │   └── sandbox.go        # Sandbox interface + tier selection
│   │   ├── herd/                 # Herd: multi-agent coordination
│   │   │   ├── manager.go        # Agent spawning + lifecycle
│   │   │   ├── router.go         # Inter-agent message routing
│   │   │   └── workflow.go       # Temporal workflow definitions
│   │   └── llm/                  # LLM provider abstraction
│   │       ├── client.go         # Unified LLM client interface
│   │       ├── streaming.go      # SSE stream processing
│   │       ├── providers/        # Provider-specific implementations
│   │       │   ├── anthropic.go
│   │       │   ├── openai.go
│   │       │   ├── google.go
│   │       │   └── ollama.go
│   │       └── failover.go       # Circuit breaker + provider rotation
│   │
│   ├── pond/                     # Memory & storage layer
│   │   ├── db.go                 # PostgreSQL connection pool (pgxpool)
│   │   ├── migrate.go            # Database migration runner
│   │   ├── ripple/               # Ripple: vector search
│   │   │   ├── index.go          # HNSW index management
│   │   │   ├── search.go         # Hybrid vector + FTS search
│   │   │   ├── embedding.go      # Embedding generation + caching
│   │   │   └── reranker.go       # MMR diversity + temporal decay
│   │   ├── pebble/               # Pebble: session persistence
│   │   │   ├── session_repo.go   # Session CRUD + indexing
│   │   │   ├── message_repo.go   # Message storage + retrieval
│   │   │   └── agent_repo.go     # Agent configuration persistence
│   │   ├── memory/               # Memory management
│   │   │   ├── manager.go        # Three-layer memory orchestrator
│   │   │   ├── episodic.go       # Episodic (conversation) memory
│   │   │   ├── semantic.go       # Semantic (knowledge) memory
│   │   │   └── procedural.go     # Procedural (skill) memory
│   │   └── migrations/           # SQL migration files
│   │       ├── 001_init.up.sql
│   │       ├── 001_init.down.sql
│   │       └── ...
│   │
│   ├── lodge/                    # Infrastructure services
│   │   ├── tide/                 # Tide: Redis pub/sub
│   │   │   ├── pubsub.go         # Cross-instance event bus
│   │   │   ├── presence.go       # Online status tracking
│   │   │   └── cache.go          # Distributed cache operations
│   │   └── drift/                # Drift: task scheduling
│   │       ├── scheduler.go      # Asynq task definitions + dispatch
│   │       ├── cron.go           # Cron job management
│   │       └── temporal.go       # Temporal client wrapper
│   │
│   ├── wetland/                  # Platform services
│   │   ├── footprint/            # Footprint: audit logging
│   │   │   ├── logger.go         # Append-only audit event writer
│   │   │   ├── events.go         # Audit event type definitions
│   │   │   └── exporter.go       # SIEM export via OTel log exporter
│   │   ├── marsh/                # Marsh: multi-tenancy
│   │   │   ├── tenant.go         # Tenant CRUD + isolation
│   │   │   ├── rbac.go           # Casbin policy management
│   │   │   ├── quota.go          # Per-tenant budget + concurrency limits
│   │   │   └── billing.go        # LLM cost tracking + alerting
│   │   ├── vapor/                # Vapor: observability
│   │   │   ├── tracer.go         # OTel tracer provider setup
│   │   │   ├── metrics.go        # Custom metrics registration
│   │   │   └── health.go         # Health check aggregator
│   │   └── canopy/               # Canopy: secret management
│   │       ├── vault.go          # HashiCorp Vault client
│   │       ├── rotation.go       # Automatic secret rotation
│   │       └── sops.go           # SOPS fallback for dev environments
│   │
│   └── shared/                   # Cross-cutting concerns
│       ├── config/               # Configuration loading + validation
│       │   ├── config.go         # Viper-based config struct
│       │   └── validate.go       # Struct validation rules
│       ├── errors/               # Structured error types
│       │   └── errors.go
│       ├── telemetry/            # OTel initialization
│       │   └── init.go
│       └── testutil/             # Shared test helpers
│           ├── fixtures.go
│           └── containers.go     # Testcontainers setup
│
├── pkg/                          # Public libraries (importable by external code)
│   ├── protocol/                 # WebSocket JSON-RPC protocol types
│   │   ├── frames.go             # Request/Response/Event frame structs
│   │   ├── methods.go            # Method name constants
│   │   └── errors.go             # Protocol error codes
│   ├── skills/                   # Skill definition types + parser
│   │   ├── skill.go              # Skill struct + YAML frontmatter parser
│   │   ├── loader.go             # Skill file discovery + loading
│   │   └── validator.go          # Skill manifest validation
│   └── mcp/                      # MCP client library
│       ├── client.go             # JSON-RPC 2.0 client
│       ├── transport.go          # stdio + SSE transports
│       └── types.go              # MCP type definitions
│
├── web/                          # Meadow: frontend application
│   ├── package.json
│   ├── next.config.ts
│   ├── tailwind.config.ts
│   ├── tsconfig.json
│   ├── src/
│   │   ├── app/                  # Next.js App Router pages
│   │   │   ├── layout.tsx
│   │   │   ├── page.tsx          # Dashboard home
│   │   │   ├── agents/           # Agent management
│   │   │   │   ├── page.tsx
│   │   │   │   └── [id]/
│   │   │   │       ├── page.tsx
│   │   │   │       └── sessions/
│   │   │   ├── sessions/         # Conversation viewer
│   │   │   │   └── [id]/page.tsx
│   │   │   ├── skills/           # Skill browser + CapyHub
│   │   │   │   └── page.tsx
│   │   │   ├── memory/           # Memory explorer
│   │   │   │   └── page.tsx
│   │   │   ├── settings/         # System configuration
│   │   │   │   ├── page.tsx
│   │   │   │   ├── channels/
│   │   │   │   ├── providers/
│   │   │   │   └── security/
│   │   │   ├── admin/            # Tenant administration
│   │   │   │   ├── tenants/
│   │   │   │   ├── users/
│   │   │   │   └── audit/
│   │   │   └── api/              # Next.js API routes (BFF)
│   │   │       └── auth/
│   │   ├── components/           
│   │   │   ├── ui/               # shadcn/ui base components
│   │   │   ├── grassland/        # Grassland: layout + navigation
│   │   │   │   ├── Sidebar.tsx
│   │   │   │   ├── Header.tsx
│   │   │   │   └── Breadcrumb.tsx
│   │   │   ├── dewdrop/          # Dewdrop: real-time components
│   │   │   │   ├── ChatStream.tsx
│   │   │   │   ├── AgentStatus.tsx
│   │   │   │   └── EventFeed.tsx
│   │   │   └── sunbeam/          # Sunbeam: visualization
│   │   │       ├── TokenUsageChart.tsx
│   │   │       ├── MemoryGraph.tsx
│   │   │       └── SessionTimeline.tsx
│   │   ├── lib/                  
│   │   │   ├── ws.ts             # WebSocket client + reconnection
│   │   │   ├── api.ts            # REST API client (fetch wrapper)
│   │   │   ├── auth.ts           # OIDC auth helpers
│   │   │   └── protocol.ts       # JSON-RPC frame types (mirrors pkg/protocol)
│   │   ├── stores/               # Zustand stores
│   │   │   ├── session.ts
│   │   │   ├── agent.ts
│   │   │   └── ui.ts
│   │   └── hooks/                # Custom React hooks
│   │       ├── useWebSocket.ts
│   │       ├── useAgent.ts
│   │       └── useStreamingChat.ts
│   └── public/
│       └── favicon.ico
│
├── deploy/                       # Deployment configurations
│   ├── docker/
│   │   ├── Dockerfile.gateway    # Multi-stage Go build
│   │   ├── Dockerfile.web        # Next.js production build
│   │   └── docker-compose.yml    # Local dev stack
│   ├── k8s/                      # Kubernetes manifests
│   │   ├── base/                 # Kustomize base
│   │   │   ├── kustomization.yaml
│   │   │   ├── namespace.yaml
│   │   │   ├── gateway-deployment.yaml
│   │   │   ├── gateway-service.yaml
│   │   │   ├── web-deployment.yaml
│   │   │   ├── web-service.yaml
│   │   │   ├── ingress.yaml
│   │   │   ├── configmap.yaml
│   │   │   ├── secrets.yaml
│   │   │   └── pvc.yaml
│   │   ├── overlays/
│   │   │   ├── dev/
│   │   │   ├── staging/
│   │   │   └── production/
│   │   └── operator/             # Future: CRD-based operator
│   └── helm/
│       └── capyclaw/
│           ├── Chart.yaml
│           ├── values.yaml
│           └── templates/
│
├── scripts/                      # Development scripts
│   ├── setup-dev.sh              # Local dev environment setup
│   ├── migrate.sh                # Database migration runner
│   ├── seed.sh                   # Test data seeding
│   └── gen-proto.sh              # Protocol type generation
│
├── docs/                         # Documentation
│   ├── architecture.md           # System architecture overview
│   ├── api-reference.md          # REST + WebSocket API docs
│   ├── security-model.md         # Security architecture deep dive
│   ├── deployment-guide.md       # Production deployment guide
│   └── contributing.md           # Contributor guidelines
│
├── tests/                        # Integration + E2E tests
│   ├── integration/
│   │   ├── gateway_test.go
│   │   ├── agent_test.go
│   │   └── memory_test.go
│   └── e2e/
│       ├── playwright.config.ts
│       └── specs/
│
├── go.mod
├── go.sum
├── Makefile
├── capyclaw.example.yaml         # Example configuration
├── .goreleaser.yml               # Release automation
└── README.md
```

---

## 4. Configuration schema

Configuration file: `capyclaw.yaml` (loaded via Viper, validated with go-playground/validator)

```yaml
# capyclaw.yaml — CapyClaw main configuration
# Environment variable override: CAPYCLAW_<SECTION>_<KEY> (e.g., CAPYCLAW_RIVERBANK_PORT=18789)

version: "1"

# ── Riverbank (Gateway) ────────────────────────────────────
riverbank:
  host: "0.0.0.0"
  port: 18789
  tls:
    enabled: true                    # REQUIRED for non-loopback. No insecure toggle.
    cert_path: "/etc/capyclaw/tls/cert.pem"
    key_path: "/etc/capyclaw/tls/key.pem"
    min_version: "1.3"
  websocket:
    max_connections: 10000
    read_buffer_size: 4096
    write_buffer_size: 4096
    ping_interval: "30s"
    pong_timeout: "10s"
    allowed_origins:                 # Explicit origin allowlist (prevents ClawJacked)
      - "https://your-domain.com"
  auth:
    provider: "oidc"                 # "oidc" | "jwt" | "device-pairing"
    oidc:
      issuer_url: "https://auth.your-domain.com"
      client_id: "capyclaw-gateway"
      client_secret: "vault://secret/capyclaw/oidc-client-secret"  # Vault reference
      scopes: ["openid", "profile", "email"]
    jwt:
      signing_method: "ES256"        # ES256 | RS256 | EdDSA
      public_key_path: "vault://secret/capyclaw/jwt-public-key"
    device_pairing:
      enabled: true
      auto_approve_loopback: true
      key_algorithm: "Ed25519"

# ── Rapids (Rate Limiting) ─────────────────────────────────
rapids:
  enabled: true
  default_limits:
    requests_per_minute: 60
    requests_per_hour: 1000
    concurrent_sessions: 5
  tenant_overrides: {}               # Per-tenant overrides loaded from DB

# ── Burrow (Agent Core) ────────────────────────────────────
burrow:
  default_model: "claude-sonnet-4-20250514"
  fallback_models:
    - "gpt-4o"
    - "gemini-2.5-flash"
  max_context_tokens: 200000
  compaction:
    strategy: "async"                # "async" | "sync" (async = zero-downtime)
    trigger_threshold: 0.80          # Trigger at 80% of context window
    summary_model: "gemini-2.5-flash"
  tool_execution:
    default_timeout: "30s"
    max_concurrent_tools: 5
  sandbox:
    default_tier: "wasm"             # "wasm" | "gvisor" | "firecracker"
    wasm:
      max_memory_mb: 256
      max_execution_time: "10s"
      allowed_host_functions: []
    gvisor:
      runtime_class: "runsc"
      max_memory_mb: 512
      max_cpu_millicores: 500
    firecracker:
      kernel_path: "/opt/firecracker/vmlinux"
      rootfs_path: "/opt/firecracker/rootfs.ext4"
      max_memory_mb: 1024
      max_vcpus: 2
      boot_timeout: "5s"

# ── Pond (Database) ────────────────────────────────────────
pond:
  postgres:
    host: "localhost"
    port: 5432
    database: "capyclaw"
    username: "capyclaw"
    password: "vault://secret/capyclaw/pg-password"
    ssl_mode: "verify-full"
    pool:
      max_conns: 50
      min_conns: 5
      max_conn_lifetime: "1h"
      max_conn_idle_time: "15m"
  vector:
    embedding_model: "text-embedding-3-small"
    embedding_dimensions: 1536
    hnsw:
      m: 16
      ef_construction: 200
      ef_search: 100
    search:
      vector_weight: 0.7
      fts_weight: 0.3
      max_results: 20
      temporal_decay_half_life: "30d"

# ── Lodge (Infrastructure) ─────────────────────────────────
lodge:
  redis:
    addresses:                       # Cluster mode
      - "redis-0:6379"
      - "redis-1:6379"
      - "redis-2:6379"
    password: "vault://secret/capyclaw/redis-password"
    db: 0
    pool_size: 100
  temporal:
    host: "temporal:7233"
    namespace: "capyclaw"
    task_queue: "capyclaw-agents"
  asynq:
    redis_addr: "redis-0:6379"       # Shares Redis cluster
    concurrency: 20
    queues:
      critical: 6
      default: 3
      low: 1

# ── Wetland (Platform Services) ────────────────────────────
wetland:
  footprint:
    enabled: true
    retention_days: 365
    export:
      enabled: false
      endpoint: "https://siem.your-domain.com/api/v1/logs"
  marsh:
    enabled: true
    default_quota:
      max_agents: 5
      max_sessions_per_agent: 100
      monthly_token_budget: 1000000
      monthly_cost_limit_usd: 50.00
      alert_threshold: 0.80          # Alert at 80% of budget
  vapor:
    otel:
      endpoint: "http://otel-collector:4317"
      protocol: "grpc"
      service_name: "capyclaw-gateway"
      sample_rate: 0.1               # 10% sampling in production
  canopy:
    provider: "vault"                # "vault" | "sops" | "env"
    vault:
      address: "https://vault:8200"
      auth_method: "kubernetes"      # "kubernetes" | "token" | "approle"
      mount_path: "secret"
      role: "capyclaw"

# ── LLM Providers ──────────────────────────────────────────
providers:
  anthropic:
    api_key: "vault://secret/capyclaw/anthropic-key"
    base_url: "https://api.anthropic.com"
    max_retries: 3
    timeout: "120s"
  openai:
    api_key: "vault://secret/capyclaw/openai-key"
    base_url: "https://api.openai.com/v1"
    max_retries: 3
    timeout: "120s"
  google:
    api_key: "vault://secret/capyclaw/google-key"
    max_retries: 3
    timeout: "120s"
  ollama:
    base_url: "http://localhost:11434"
    timeout: "300s"

# ── Whiskers (Channel Adapters) ────────────────────────────
whiskers:
  telegram:
    enabled: false
    bot_token: "vault://secret/capyclaw/telegram-token"
  discord:
    enabled: false
    bot_token: "vault://secret/capyclaw/discord-token"
    guild_id: ""
  slack:
    enabled: false
    bot_token: "vault://secret/capyclaw/slack-bot-token"
    app_token: "vault://secret/capyclaw/slack-app-token"
  # Additional adapters follow the same pattern
```

---

## 5. Database schema

### 5.1 Core tables

```sql
-- ============================================================
-- CapyClaw Database Schema (PostgreSQL 16 + pgvector 0.8)
-- ============================================================

-- Extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";

-- ── Marsh: Tenants ──────────────────────────────────────────
CREATE TABLE tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) NOT NULL UNIQUE,
    plan VARCHAR(50) NOT NULL DEFAULT 'free',
    settings JSONB NOT NULL DEFAULT '{}',
    quota JSONB NOT NULL DEFAULT '{
        "max_agents": 5,
        "max_sessions_per_agent": 100,
        "monthly_token_budget": 1000000,
        "monthly_cost_limit_usd": 50.00
    }',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    external_id VARCHAR(255),          -- OIDC subject ID
    email VARCHAR(320) NOT NULL,
    display_name VARCHAR(255),
    role VARCHAR(50) NOT NULL DEFAULT 'operator',  -- admin | operator | viewer
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, email)
);

CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_name VARCHAR(255) NOT NULL,
    public_key TEXT NOT NULL,          -- Ed25519 public key (PEM)
    platform VARCHAR(50),             -- macos | linux | windows | ios | android
    role VARCHAR(20) NOT NULL DEFAULT 'operator',
    scopes TEXT[] NOT NULL DEFAULT '{"operator.read","operator.write"}',
    paired_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

-- ── Pebble: Agents & Sessions ───────────────────────────────
CREATE TABLE agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) NOT NULL,
    model VARCHAR(100) NOT NULL DEFAULT 'claude-sonnet-4-20250514',
    fallback_models TEXT[] DEFAULT '{}',
    system_prompt TEXT,
    identity_md TEXT,                  -- IDENTITY.md content
    soul_md TEXT,                      -- SOUL.md content
    user_md TEXT,                      -- USER.md content
    settings JSONB NOT NULL DEFAULT '{}',
    tool_policy JSONB NOT NULL DEFAULT '{"default": "ask"}',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, slug)
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_key VARCHAR(500) NOT NULL, -- e.g., "direct:telegram:12345"
    origin JSONB NOT NULL DEFAULT '{}',
    compaction_count INT NOT NULL DEFAULT 0,
    total_input_tokens BIGINT NOT NULL DEFAULT 0,
    total_output_tokens BIGINT NOT NULL DEFAULT 0,
    total_cost_usd DECIMAL(12,6) NOT NULL DEFAULT 0,
    context_summary TEXT,             -- Latest compaction summary
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(agent_id, session_key)
);
CREATE INDEX idx_sessions_tenant ON sessions(tenant_id);
CREATE INDEX idx_sessions_agent ON sessions(agent_id);
CREATE INDEX idx_sessions_updated ON sessions(updated_at DESC);

CREATE TABLE messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL,
    role VARCHAR(20) NOT NULL,        -- user | assistant | system | tool_result
    content JSONB NOT NULL,           -- Array of content blocks
    model VARCHAR(100),
    usage JSONB,                      -- {input_tokens, output_tokens, cost_usd}
    tool_calls JSONB,                 -- Tool invocations in this message
    parent_id UUID REFERENCES messages(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_messages_session ON messages(session_id, created_at);
CREATE INDEX idx_messages_tenant ON messages(tenant_id);

-- ── Ripple: Memory ──────────────────────────────────────────
CREATE TABLE memories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    memory_type VARCHAR(50) NOT NULL,  -- episodic | semantic | procedural
    content TEXT NOT NULL,
    embedding vector(1536),
    metadata JSONB NOT NULL DEFAULT '{}',
    importance_score FLOAT NOT NULL DEFAULT 0.5,
    source_session_id UUID REFERENCES sessions(id),
    source_message_id UUID REFERENCES messages(id),
    access_count INT NOT NULL DEFAULT 0,
    last_accessed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_memories_hnsw ON memories
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 200);
CREATE INDEX idx_memories_fts ON memories
    USING GIN (to_tsvector('english', content));
CREATE INDEX idx_memories_agent ON memories(agent_id, memory_type);
CREATE INDEX idx_memories_tenant ON memories(tenant_id);

-- ── Skills ──────────────────────────────────────────────────
CREATE TABLE skills (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE,  -- NULL = global/bundled
    name VARCHAR(255) NOT NULL,
    version VARCHAR(50) NOT NULL DEFAULT '1.0.0',
    description TEXT,
    manifest JSONB NOT NULL,          -- Parsed SKILL.md frontmatter
    content_md TEXT NOT NULL,          -- SKILL.md body
    source VARCHAR(50) NOT NULL,      -- bundled | capyhub | workspace
    verified BOOLEAN NOT NULL DEFAULT false,
    sandbox_tier VARCHAR(20) NOT NULL DEFAULT 'wasm',
    installed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_skills_tenant ON skills(tenant_id);

CREATE TABLE agent_skills (
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    skill_id UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INT NOT NULL DEFAULT 0,
    config JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (agent_id, skill_id)
);

-- ── Drift: Cron Jobs ────────────────────────────────────────
CREATE TABLE cron_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    schedule VARCHAR(100) NOT NULL,    -- Cron expression
    prompt TEXT NOT NULL,              -- Message to send to agent
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    run_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Footprint: Audit Log ────────────────────────────────────
-- INSERT-only table. No UPDATE or DELETE grants.
CREATE TABLE audit_log (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID NOT NULL,
    actor_id UUID,                     -- User ID (NULL for system events)
    actor_type VARCHAR(20) NOT NULL,   -- user | agent | system | webhook
    action VARCHAR(100) NOT NULL,      -- e.g., "tool.execute", "session.create"
    resource_type VARCHAR(50),         -- e.g., "agent", "session", "skill"
    resource_id UUID,
    details JSONB NOT NULL DEFAULT '{}',
    ip_address INET,
    user_agent TEXT,
    request_id VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_tenant_time ON audit_log(tenant_id, created_at DESC);
CREATE INDEX idx_audit_actor ON audit_log(actor_id, created_at DESC);
CREATE INDEX idx_audit_action ON audit_log(action, created_at DESC);

-- ── Whiskers: Channel Connections ───────────────────────────
CREATE TABLE channel_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_type VARCHAR(50) NOT NULL, -- telegram | discord | slack | whatsapp | ...
    account_id VARCHAR(255) NOT NULL,  -- Bot/account identifier
    config JSONB NOT NULL DEFAULT '{}',
    credentials_ref VARCHAR(500),      -- Vault path reference
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    last_connected_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Agent Bindings (route conversations to agents) ──────────
CREATE TABLE agent_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    channel_connection_id UUID NOT NULL REFERENCES channel_connections(id) ON DELETE CASCADE,
    match_pattern JSONB NOT NULL DEFAULT '{}',  -- Channel/group/peer patterns
    priority INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT true
);
```

### 5.2 Row-Level Security (multi-tenancy)

```sql
-- Enable RLS on all tenant-scoped tables
ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE memories ENABLE ROW LEVEL SECURITY;

-- Policy: users only see data from their own tenant
-- The app sets current_setting('app.tenant_id') on each connection
CREATE POLICY tenant_isolation ON agents
    USING (tenant_id = current_setting('app.tenant_id')::UUID);
CREATE POLICY tenant_isolation ON sessions
    USING (tenant_id = current_setting('app.tenant_id')::UUID);
CREATE POLICY tenant_isolation ON messages
    USING (tenant_id = current_setting('app.tenant_id')::UUID);
CREATE POLICY tenant_isolation ON memories
    USING (tenant_id = current_setting('app.tenant_id')::UUID);
```

---

## 6. API specification

### 6.1 REST API (HTTP)

Base URL: `https://{host}:{port}`

#### Health

```
GET  /healthz              → 200 {"status":"ok"}
GET  /readyz               → 200 {"status":"ready"} | 503
```

#### Chat completions (OpenAI-compatible)

```
POST /v1/chat/completions
Authorization: Bearer {token}
X-Tenant-ID: {tenant_id}
Content-Type: application/json

{
  "model": "{agent_slug}",          // Agent slug used as model identifier
  "messages": [
    {"role": "user", "content": "Hello, what can you do?"}
  ],
  "stream": true,                   // SSE streaming
  "max_tokens": 4096,
  "temperature": 0.7
}

→ 200 (SSE stream)
data: {"id":"msg_xxx","object":"chat.completion.chunk","choices":[{"delta":{"content":"I can..."}}]}
data: [DONE]
```

#### Agents

```
GET    /api/v1/agents                → List agents
POST   /api/v1/agents                → Create agent
GET    /api/v1/agents/:id            → Get agent details
PATCH  /api/v1/agents/:id            → Update agent
DELETE /api/v1/agents/:id            → Delete agent
GET    /api/v1/agents/:id/sessions   → List agent sessions
POST   /api/v1/agents/:id/skills     → Attach skill to agent
DELETE /api/v1/agents/:id/skills/:sid → Detach skill
```

#### Sessions

```
GET    /api/v1/sessions/:id           → Get session details
GET    /api/v1/sessions/:id/messages  → List session messages (paginated)
DELETE /api/v1/sessions/:id           → Archive session
POST   /api/v1/sessions/:id/compact   → Trigger manual compaction
```

#### Memory

```
GET    /api/v1/agents/:id/memories           → Search memories (query param: q, type, limit)
POST   /api/v1/agents/:id/memories           → Create memory entry
DELETE /api/v1/agents/:id/memories/:mid       → Delete memory
POST   /api/v1/agents/:id/memories/search     → Hybrid vector + FTS search
```

#### Skills

```
GET    /api/v1/skills                  → List installed skills
POST   /api/v1/skills/install          → Install from CapyHub
POST   /api/v1/skills/upload           → Upload workspace skill
DELETE /api/v1/skills/:id              → Uninstall skill
GET    /api/v1/capyhub/search          → Search CapyHub marketplace
```

#### Tools (Direct invocation)

```
POST /tools/invoke
Authorization: Bearer {token}

{
  "tool": "web_search",
  "input": {"query": "latest AI news"},
  "agent_id": "uuid",
  "session_id": "uuid"              // Optional: associates with session
}

→ 200 {"result": {...}, "duration_ms": 342}
```

#### Webhooks

```
POST /hooks/{path}
X-Hook-Secret: {shared_secret}
Content-Type: application/json

{body}

→ 200 {"accepted": true, "agent_id": "uuid", "session_id": "uuid"}
```

#### Administration

```
GET    /api/v1/admin/tenants           → List tenants
POST   /api/v1/admin/tenants           → Create tenant
PATCH  /api/v1/admin/tenants/:id       → Update tenant
GET    /api/v1/admin/tenants/:id/usage → Get tenant usage stats
GET    /api/v1/admin/audit             → Query audit log
GET    /api/v1/admin/health/detailed   → Detailed system health
```

### 6.2 WebSocket protocol (JSON-RPC)

Endpoint: `wss://{host}:{port}/ws`

#### Frame types

```typescript
// Request (client → server)
{
  "type": "req",
  "id": "uuid-v4",
  "method": "chat.send",
  "params": { ... }
}

// Response (server → client)
{
  "type": "res",
  "id": "uuid-v4",       // Matches request ID
  "ok": true,
  "payload": { ... }
}
// Error response
{
  "type": "res",
  "id": "uuid-v4",
  "ok": false,
  "error": {
    "code": 4001,
    "message": "Rate limit exceeded",
    "details": { "retry_after_ms": 5000 }
  }
}

// Event (server → client, unsolicited)
{
  "type": "event",
  "event": "agent.message.chunk",
  "payload": { ... },
  "seq": 42              // Monotonically increasing sequence number
}
```

#### Handshake flow

```
1. Client connects to wss://{host}/ws
2. Server sends:   {"type":"event","event":"connect.challenge","payload":{"nonce":"xxx","protocol_versions":[3]}}
3. Client sends:   {"type":"req","id":"1","method":"connect","params":{
                      "protocol_version": 3,
                      "client": {"name":"meadow","version":"0.1.0","platform":"web"},
                      "role": "operator",
                      "scopes": ["operator.read","operator.write"],
                      "auth": {
                        "method": "jwt",
                        "token": "eyJ..."
                      },
                      "device": {
                        "id": "device-uuid",
                        "signature": "base64(sign(nonce + device_id + timestamp))"
                      }
                    }}
4. Server sends:   {"type":"res","id":"1","ok":true,"payload":{
                      "protocol_version": 3,
                      "user": {"id":"uuid","email":"...","role":"operator"},
                      "tenant": {"id":"uuid","name":"..."},
                      "tick_interval_ms": 30000
                    }}
```

#### Core methods

```
# Session management
chat.send           → Send message to agent session
chat.cancel         → Cancel in-progress generation
sessions.list       → List active sessions
sessions.get        → Get session with recent messages
sessions.create     → Create new session
sessions.archive    → Archive session

# Agent management
agents.list         → List configured agents
agents.get          → Get agent configuration
agents.update       → Update agent settings

# Tool interaction
tools.approve       → Approve pending tool execution
tools.reject        → Reject pending tool execution
tools.list          → List available tools for agent

# Real-time events (server → client)
agent.message.chunk → Streaming token from LLM
agent.message.done  → Message generation complete
agent.tool.request  → Tool execution pending approval
agent.tool.result   → Tool execution result
agent.error         → Agent error
session.updated     → Session metadata changed
presence.update     → User online/offline status
```

#### Error codes

```
4000  Bad Request          — Malformed frame or missing required field
4001  Rate Limited         — Per-tenant rate limit exceeded
4003  Forbidden            — Insufficient scopes for requested method
4004  Not Found            — Session/agent/resource not found
4010  Auth Required        — Missing or expired authentication
4011  Auth Failed          — Invalid credentials or signature
4012  Device Not Paired    — Device requires pairing approval
4020  Tenant Suspended     — Tenant account suspended
4030  Budget Exceeded      — Monthly token/cost budget exhausted
5000  Internal Error       — Unexpected server error
5001  Provider Unavailable — All LLM providers failing (circuit open)
5002  Sandbox Error        — Tool sandbox initialization failed
```

---

## 7. Security architecture

### 7.1 Authentication flow

```
┌─────────┐     ┌──────────┐     ┌──────────┐     ┌─────────┐
│  Client  │────→│  Sentry  │────→│   OIDC   │────→│  Vault  │
│ (Meadow) │←────│(Riverbank│←────│ Provider │     │(Canopy) │
└─────────┘     │ middleware)     └──────────┘     └─────────┘
                └──────────┘
                     │
                     ▼
              ┌──────────┐
              │  Casbin   │  ← RBAC/ABAC policy evaluation
              │  (Marsh)  │
              └──────────┘
```

Rules:
- All non-loopback connections require TLS 1.3+
- No "insecure auth" configuration option exists
- WebSocket Origin header validated against explicit allowlist
- `gatewayUrl` never accepted from URL query parameters
- Rate limiting applies to ALL connections including loopback
- Device pairing uses Ed25519 challenge-response
- JWT tokens are short-lived (15 min), refresh via OIDC

### 7.2 Tool execution sandbox tiers

| Tier | Technology | Use Case | Startup | Memory | Capabilities |
|------|-----------|----------|---------|--------|-------------|
| 1 | Wazero (WASM) | Verified CapyHub skills | ~1ms | ≤256 MB | Host function allowlist only |
| 2 | gVisor (runsc) | User-installed tools | ~500ms | ≤512 MB | Limited syscalls, no network by default |
| 3 | Firecracker | Untrusted code execution | ~125ms | ≤1 GB | Full OS isolation, ephemeral |

Tier selection logic:
```
if skill.source == "bundled" || (skill.source == "capyhub" && skill.verified):
    tier = WASM
elif skill.source == "workspace" || skill.source == "capyhub":
    tier = gVisor
else:  # exec tools, browser automation, untrusted MCP
    tier = Firecracker
```

### 7.3 Command execution safety

- No shell expansion. Commands exec'd via `syscall.Exec` with argument arrays
- `safeBins` replaced by seccomp-BPF profiles per binary
- Read-only root filesystem in all sandboxed executions
- Namespace isolation (PID, NET, MNT, USER) for Tier 2+
- All tool invocations logged to `audit_log` BEFORE execution

### 7.4 MCP server trust model

- All MCP connections require mutual TLS
- Tool descriptions sanitized to prevent prompt injection
- Per-tool resource quotas enforced at gateway level
- Tool call results are truncated to configurable max size
- MCP servers from CapyHub must pass automated security scan

---

## 8. Agent execution pipeline

Every message flows through an 8-stage pipeline implemented as chained Go middleware:

```go
// internal/burrow/pipeline.go
type Stage interface {
    Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error)
}

type Pipeline struct {
    stages []Stage
}

// Stages execute in order:
// 1. SessionResolver   → Determine/create session from channel context
// 2. WorkspaceLoader   → Load agent's identity, skills, boot files
// 3. ModelSelector     → Resolve model + auth profile + fallback chain
// 4. PromptBuilder     → Assemble system prompt (identity + skills XML + memory)
// 5. ToolPolicyFilter  → Apply cascading tool policy (global → tenant → agent → channel)
// 6. LLMInvoker        → Stream request to selected provider
// 7. ToolExecutor      → Execute tool calls in sandbox, return results, loop
// 8. SessionPersister  → Write messages to DB, update token counts, trigger memory flush
```

Context compaction (Instinct module) runs asynchronously:

```
Main goroutine:  [user msg] → [pipeline stages 1-8] → [response to user]
                                    ↓ (if tokens > 80% threshold)
Compaction goroutine:  [summarize old messages] → [update session.context_summary]
                       [clear tool results] → [update token counts]
```

---

## 9. Development workflow

### 9.1 Local setup

```bash
# Prerequisites: Go 1.23+, Node.js 22+, Docker, pnpm

# Clone and setup
git clone https://github.com/your-org/capyclaw.git
cd capyclaw
make setup          # Installs tools, initializes git hooks

# Start infrastructure (PostgreSQL, Redis, Temporal)
make infra-up       # docker compose -f deploy/docker/docker-compose.yml up -d

# Run database migrations
make migrate-up     # go run cmd/capy/main.go migrate up

# Seed development data
make seed           # go run scripts/seed.go

# Start backend (with hot reload via air)
make dev-gateway    # air -c .air.toml

# Start frontend (in separate terminal)
make dev-web        # cd web && pnpm dev

# Run all tests
make test           # go test ./...
make test-web       # cd web && pnpm test

# Run linters
make lint           # golangci-lint run && cd web && pnpm lint
```

### 9.2 Makefile targets

```makefile
# Development
make dev-gateway       # Start gateway with hot reload
make dev-web           # Start Next.js dev server
make infra-up          # Start Docker infrastructure
make infra-down        # Stop Docker infrastructure

# Database
make migrate-up        # Run all pending migrations
make migrate-down      # Rollback last migration
make migrate-create    # Create new migration (NAME=xxx)
make seed              # Seed development data

# Testing
make test              # Run Go unit tests
make test-integration  # Run integration tests (requires infra)
make test-e2e          # Run Playwright E2E tests
make test-all          # Run everything

# Build
make build             # Build gateway + CLI binaries
make build-web         # Build Next.js production bundle
make docker-build      # Build Docker images

# Code quality
make lint              # Run golangci-lint + eslint
make fmt               # Format Go + TS code
make generate          # Run go generate (mocks, types)

# Release
make release           # GoReleaser production build
```

### 9.3 Testing strategy

```
tests/
├── Unit tests:        internal/**/*_test.go (per-package, mocked deps)
├── Integration tests: tests/integration/ (real PostgreSQL + Redis via testcontainers)
├── E2E tests:         tests/e2e/ (Playwright, full stack)
└── Benchmarks:        internal/**/*_bench_test.go (performance regression tracking)
```

Rules:
- Every exported function needs a unit test
- Repository implementations need integration tests against real PostgreSQL
- WebSocket handler tests use in-memory connection mocks
- LLM provider tests use recorded HTTP fixtures (no live API calls in CI)
- Sandbox tests run in a dedicated CI environment with gVisor/Firecracker support

### 9.4 Code conventions

**Go:**
- Follow effective Go and the Go Code Review Comments guide
- Use `context.Context` as first parameter everywhere
- Use `slog` for structured logging (no `fmt.Println` or `log.Printf`)
- Errors wrap with `fmt.Errorf("operation: %w", err)` for stack traces
- Repository pattern for all database access (interface + implementation)
- Table-driven tests with `t.Run()` subtests

**TypeScript (frontend):**
- Strict TypeScript with no `any` types
- React Server Components for data fetching, Client Components for interactivity
- Zustand for client-side state, React Query for server state
- All API calls go through typed client functions in `lib/api.ts`

**Both:**
- Conventional Commits for commit messages (`feat:`, `fix:`, `chore:`, `docs:`)
- PR reviews required before merge
- CI must pass (lint + test + build) before merge

---

## 10. Deployment

### 10.1 Docker Compose (development / single-node)

```yaml
# deploy/docker/docker-compose.yml
services:
  gateway:
    build:
      context: ../../../../Users
      dockerfile: deploy/docker/Dockerfile.gateway
    ports:
      - "18789:18789"
    environment:
      - CAPYCLAW_POND_POSTGRES_HOST=postgres
      - CAPYCLAW_LODGE_REDIS_ADDRESSES=redis:6379
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy

  web:
    build:
      context: ../../../../Users
      dockerfile: deploy/docker/Dockerfile.web
    ports:
      - "3000:3000"
    environment:
      - NEXT_PUBLIC_GATEWAY_URL=ws://localhost:18789/ws
      - NEXT_PUBLIC_API_URL=http://localhost:18789

  postgres:
    image: pgvector/pgvector:pg16
    environment:
      POSTGRES_DB: capyclaw
      POSTGRES_USER: capyclaw
      POSTGRES_PASSWORD: devpassword
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: [ "CMD-SHELL", "pg_isready -U capyclaw" ]
      interval: 5s

  redis:
    image: redis:7.4-alpine
    ports:
      - "6379:6379"
    healthcheck:
      test: [ "CMD", "redis-cli", "ping" ]
      interval: 5s

  temporal:
    image: temporalio/auto-setup:1.25
    ports:
      - "7233:7233"
      - "8233:8233"
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  pgdata:
```

### 10.2 Kubernetes (production)

Key manifests in `deploy/k8s/base/`:

**Gateway Deployment:**
- Replicas: 3+ (HPA based on CPU + active WebSocket connections)
- Resource requests: 256m CPU, 512Mi memory
- Resource limits: 1 CPU, 1Gi memory
- Liveness probe: `/healthz` every 10s
- Readiness probe: `/readyz` every 5s
- Graceful shutdown: `terminationGracePeriodSeconds: 60`
- Anti-affinity: prefer different nodes

**Web Deployment:**
- Replicas: 2+
- Resource requests: 128m CPU, 256Mi memory
- Standard Next.js production container

**Ingress:**
- TLS termination at ingress controller
- WebSocket upgrade support (`nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"`)
- Session affinity for WebSocket connections (`nginx.ingress.kubernetes.io/affinity: "cookie"`)

**PostgreSQL:**
- Use CloudNativePG operator or managed service (RDS, Cloud SQL)
- HA with streaming replication
- pgvector extension pre-installed
- Automated backups to S3/GCS

**Redis:**
- Redis Cluster mode with 3+ master nodes
- Sentinel for automatic failover
- Or managed service (ElastiCache, Memorystore)

### 10.3 Resource requirements

| Component | Min (dev) | Recommended (prod) |
|-----------|-----------|-------------------|
| Gateway (per instance) | 1 CPU, 512Mi | 2 CPU, 2Gi |
| Web (per instance) | 0.5 CPU, 256Mi | 1 CPU, 1Gi |
| PostgreSQL | 2 CPU, 4Gi | 4 CPU, 16Gi |
| Redis | 1 CPU, 1Gi | 2 CPU, 4Gi |
| Temporal | 1 CPU, 2Gi | 2 CPU, 4Gi |
| Total minimum | 5.5 CPU, 8Gi | 13 CPU, 31Gi |

---

## 11. Observability

### 11.1 Metrics (Prometheus)

Key custom metrics exposed at `/metrics`:

```
# Gateway
capyclaw_ws_connections_active{tenant_id}          gauge
capyclaw_ws_messages_total{tenant_id,direction}    counter
capyclaw_http_requests_total{method,path,status}   counter
capyclaw_http_request_duration_seconds{method,path} histogram

# Agent
capyclaw_agent_sessions_active{agent_id}           gauge
capyclaw_agent_pipeline_duration_seconds{stage}     histogram
capyclaw_agent_tool_executions_total{tool,tier}     counter
capyclaw_agent_tool_execution_duration{tool,tier}   histogram

# LLM
capyclaw_llm_requests_total{provider,model,status}  counter
capyclaw_llm_tokens_total{provider,model,direction}  counter
capyclaw_llm_cost_usd_total{provider,model}          counter
capyclaw_llm_latency_seconds{provider,model}         histogram
capyclaw_llm_circuit_state{provider}                 gauge  # 0=closed 1=half-open 2=open

# Memory
capyclaw_memory_search_duration_seconds{type}       histogram
capyclaw_memory_entries_total{agent_id,type}         gauge

# Tenant
capyclaw_tenant_token_usage{tenant_id,month}         gauge
capyclaw_tenant_cost_usd{tenant_id,month}            gauge
capyclaw_tenant_rate_limit_hits{tenant_id}           counter
```

### 11.2 Tracing (OpenTelemetry)

Every request generates a trace spanning:
1. HTTP/WebSocket ingress (Sentry middleware)
2. Auth validation
3. Rate limit check (Rapids)
4. Pipeline stages (each stage = child span)
5. LLM API call (including streaming duration)
6. Tool execution (per-tool child spans with sandbox tier tag)
7. Database queries (pgx tracing integration)
8. Redis operations
9. Response delivery

Trace context propagates via W3C TraceContext headers for HTTP and via `trace_id` field in WebSocket frames.

### 11.3 Logging (structured, slog)

```go
// Standard log format
slog.Info("message processed",
    "tenant_id", tenantID,
    "agent_id", agentID,
    "session_id", sessionID,
    "model", model,
    "input_tokens", usage.InputTokens,
    "output_tokens", usage.OutputTokens,
    "duration_ms", elapsed.Milliseconds(),
    "trace_id", span.SpanContext().TraceID().String(),
)
```

Log levels: DEBUG (dev only), INFO (normal ops), WARN (degraded), ERROR (failures).
All logs include `tenant_id`, `request_id`, and `trace_id` for correlation.

---

## 12. Migration from OpenClaw

CapyClaw provides import tools for teams migrating from OpenClaw:

```bash
# Import OpenClaw configuration
capy migrate import-config --from ~/.openclaw/openclaw.json

# Import session history (JSONL → PostgreSQL)
capy migrate import-sessions --from ~/.openclaw/agents/*/sessions/

# Import memory files (Markdown → memories table)
capy migrate import-memory --from ~/.openclaw/workspace-*/memory/

# Import skills (SKILL.md files → skills table)
capy migrate import-skills --from ~/.openclaw/skills/

# Validate imported data
capy migrate verify
```

The import preserves session IDs, timestamps, and message content. Token counts are recalculated against the database schema. Vault references replace any plaintext credentials found in the OpenClaw config.

---

## 13. Roadmap

### Phase 1 — Foundation (Weeks 1-6)
- [ ] Project scaffolding (directory structure, Go modules, Next.js init)
- [ ] Pond: PostgreSQL schema + migrations + repository layer
- [ ] Riverbank: HTTP server + health endpoints + auth middleware
- [ ] Riverbank: WebSocket server + JSON-RPC protocol + handshake
- [ ] Burrow: Basic agent lifecycle + single-model LLM invocation
- [ ] Meadow: Login + dashboard shell + agent list

### Phase 2 — Core Agent (Weeks 7-12)
- [ ] Burrow: Full 8-stage pipeline
- [ ] Instinct: System prompt assembly + async compaction
- [ ] Nibble: Tool execution with Wazero WASM sandbox
- [ ] Ripple: pgvector hybrid search + embedding pipeline
- [ ] Pebble: Session persistence + message streaming
- [ ] Meadow: Chat interface + streaming + session viewer

### Phase 3 — Channels & Skills (Weeks 13-18)
- [ ] Whiskers: Telegram, Discord, Slack adapters
- [ ] Skills framework: SKILL.md parser + loader + CapyHub client
- [ ] Herd: Multi-agent spawning + inter-agent routing
- [ ] Drift: Cron jobs + Asynq task queue
- [ ] Mudbath: gVisor Tier 2 sandbox integration

### Phase 4 — Enterprise (Weeks 19-24)
- [ ] Marsh: Full multi-tenancy + RBAC + quota management
- [ ] Footprint: Audit logging + SIEM export
- [ ] Canopy: Vault integration + secret rotation
- [ ] Vapor: Full OTel tracing + Grafana dashboards
- [ ] Mudbath: Firecracker Tier 3 integration
- [ ] Kubernetes operator (CRD-based lifecycle management)

### Phase 5 — Ecosystem (Weeks 25+)
- [ ] CapyHub marketplace (verified skill registry)
- [ ] WhatsApp, WeChat, Signal, Matrix adapters
- [ ] Mobile apps (iOS/Android) via WebSocket client
- [ ] MCP server hosting (run MCP servers in sandbox)
- [ ] Plugin SDK for third-party Go extensions
- [ ] Horizontal auto-scaling with session migration

---

## Appendix A: Environment variables

All config values can be overridden via environment variables using the pattern:
`CAPYCLAW_{SECTION}_{KEY}` (uppercase, underscores replacing dots)

```bash
# Examples
CAPYCLAW_RIVERBANK_PORT=18789
CAPYCLAW_RIVERBANK_TLS_ENABLED=true
CAPYCLAW_POND_POSTGRES_HOST=db.example.com
CAPYCLAW_POND_POSTGRES_PASSWORD=vault://secret/capyclaw/pg-password
CAPYCLAW_LODGE_REDIS_ADDRESSES=redis-0:6379,redis-1:6379,redis-2:6379
CAPYCLAW_BURROW_DEFAULT_MODEL=claude-sonnet-4-20250514
CAPYCLAW_WETLAND_VAPOR_OTEL_ENDPOINT=http://otel-collector:4317
```

## Appendix B: CLI reference

```bash
capy                              # Show help
capy version                      # Print version info
capy start                        # Start gateway server
capy start --config /path/to.yaml # Start with custom config

# Agent management
capy agent list                   # List agents
capy agent create <name>          # Create agent interactively
capy agent delete <slug>          # Delete agent
capy agent chat <slug>            # Start interactive CLI chat

# Session management
capy session list <agent-slug>    # List sessions
capy session replay <session-id>  # Replay session in terminal
capy session export <session-id>  # Export session as JSONL

# Herd (multi-agent)
capy herd status                  # Show all running agents
capy herd spawn <agent-slug>      # Spawn sub-agent
capy herd send <agent> <message>  # Send message to agent

# Skills
capy skill list                   # List installed skills
capy skill install <name>         # Install from CapyHub
capy skill search <query>         # Search CapyHub
capy skill verify <path>          # Verify skill integrity

# Database
capy migrate up                   # Run pending migrations
capy migrate down                 # Rollback last migration
capy migrate status               # Show migration status

# Security
capy security audit               # Run security audit
capy security rotate-keys         # Rotate JWT signing keys
capy device list                  # List paired devices
capy device revoke <device-id>    # Revoke device

# Import (from OpenClaw)
capy migrate import-config --from <path>
capy migrate import-sessions --from <path>
capy migrate import-memory --from <path>
```

## Appendix C: Go interface definitions

These interfaces define the contracts between layers. Implementations live in the corresponding `internal/` packages.

```go
// ── Burrow interfaces ───────────────────────────────────────

// Agent represents a configured AI agent
type Agent interface {
    ID() uuid.UUID
    TenantID() uuid.UUID
    Slug() string
    ProcessMessage(ctx context.Context, msg *IncomingMessage) (<-chan *StreamChunk, error)
    Shutdown(ctx context.Context) error
}

// SessionManager manages agent sessions
type SessionManager interface {
    GetOrCreate(ctx context.Context, agentID uuid.UUID, key string, origin Origin) (*Session, error)
    Get(ctx context.Context, sessionID uuid.UUID) (*Session, error)
    List(ctx context.Context, agentID uuid.UUID, opts ListOpts) ([]*Session, error)
    Archive(ctx context.Context, sessionID uuid.UUID) error
}

// ToolExecutor executes tools in sandboxed environments
type ToolExecutor interface {
    Execute(ctx context.Context, call *ToolCall, tier SandboxTier) (*ToolResult, error)
    ListAvailable(ctx context.Context, agentID uuid.UUID) ([]*ToolDef, error)
}

// LLMClient abstracts LLM provider communication
type LLMClient interface {
    Stream(ctx context.Context, req *LLMRequest) (<-chan *LLMChunk, error)
    CountTokens(ctx context.Context, messages []Message) (int, error)
    Provider() string
}

// ── Pond interfaces ─────────────────────────────────────────

// MemoryStore manages agent memory with hybrid search
type MemoryStore interface {
    Store(ctx context.Context, entry *MemoryEntry) error
    Search(ctx context.Context, query MemoryQuery) ([]*MemoryResult, error)
    Delete(ctx context.Context, id uuid.UUID) error
    Compact(ctx context.Context, agentID uuid.UUID) error
}

// MessageRepository persists conversation messages
type MessageRepository interface {
    Append(ctx context.Context, msg *Message) error
    ListBySession(ctx context.Context, sessionID uuid.UUID, opts PaginationOpts) ([]*Message, error)
    CountTokens(ctx context.Context, sessionID uuid.UUID) (TokenCounts, error)
}

// ── Wetland interfaces ──────────────────────────────────────

// AuditLogger writes immutable audit events
type AuditLogger interface {
    Log(ctx context.Context, event *AuditEvent) error
    Query(ctx context.Context, filter AuditFilter) ([]*AuditEvent, error)
}

// TenantManager handles multi-tenant isolation
type TenantManager interface {
    Get(ctx context.Context, id uuid.UUID) (*Tenant, error)
    CheckQuota(ctx context.Context, tenantID uuid.UUID, usage UsageRequest) (bool, error)
    RecordUsage(ctx context.Context, tenantID uuid.UUID, usage Usage) error
}
```

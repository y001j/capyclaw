# CapyClaw

<p align="center">
  <strong>Security-First, Horizontally-Scalable AI Agent Platform</strong>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> |
  <a href="#architecture">Architecture</a> |
  <a href="#development">Development</a> |
  <a href="docs/capyclaw-framework-guide.md">Framework Guide</a> |
  <a href="docs/CapyClaw_Research.md">Security Research</a> |
  <a href="README_CN.md">中文文档</a>
</p>

---

## Overview

CapyClaw is a ground-up Go + Next.js reimplementation of OpenClaw, designed to eliminate 13+ CVEs and the architectural limitations of the original Node.js system. It provides a personal, always-on AI assistant that connects to 25+ messaging platforms, executes autonomous tasks via LLMs, and supports multi-tenant enterprise deployment.

**Core design principles:**

- **Security by default** — mTLS/OIDC everywhere, no insecure auth toggles, secrets never touch disk in plaintext
- **Stateless compute** — any instance can serve any session; all state lives in PostgreSQL + Redis
- **Goroutine-per-session** — 2-4 KB stack per session, no event-loop blocking
- **Tiered sandboxing** — WASM (Wazero) → gVisor → Firecracker, escalating by trust level
- **Observable by default** — OpenTelemetry traces, metrics, and logs from day one

> **Status:** v0.1.0-alpha — core scaffolding, config, database schema, middleware, 8-stage agent pipeline, LLM providers (Anthropic/OpenAI), MCP integration (stdio + SSE), multi-agent coordination (sequential/parallel/supervisor workflows), channel adapters (Telegram/Discord/Slack + webhook), tiered sandboxing (WASM/gVisor/Firecracker), Cron scheduling, and Temporal durable workflows are implemented. Google/Ollama providers, CapyHub marketplace, and frontend UI are in progress.

## Features

- **Multi-LLM support** — Anthropic (Claude), OpenAI, Google (Gemini), Ollama (local models)
- **8-stage agent pipeline** — SessionResolver → WorkspaceLoader → ModelSelector → PromptBuilder → ToolPolicyFilter → LLMInvoker → ToolExecutor → SessionPersister
- **Three-layer memory** — episodic, semantic, and procedural memory with pgvector hybrid search (vector + full-text)
- **Channel adapters** — Telegram, Discord, Slack, WhatsApp, WeChat, Signal, Matrix
- **OpenAI-compatible API** — `POST /v1/chat/completions` drop-in compatibility
- **MCP protocol support** — Model Context Protocol client for tool integrations
- **Multi-agent coordination** — agent spawning, inter-agent routing, Temporal durable workflows
- **Enterprise multi-tenancy** — row-level security, per-tenant rate limiting, RBAC/ABAC via Casbin
- **Async context compaction** — background summarization at 80% token threshold, no 30-second freezes

## Architecture

All internal packages follow a **capybara ecology** naming theme:

| Codename | Path | Role |
|----------|------|------|
| **Riverbank** | `internal/riverbank/` | HTTP/WebSocket gateway, routing, middleware, channel adapters |
| **Burrow** | `internal/burrow/` | Agent runtime, LLM providers, sandboxing, tool execution, multi-agent coordination |
| **Pond** | `internal/pond/` | PostgreSQL + pgvector storage, migrations, vector search (Ripple) |
| **Lodge** | `internal/lodge/` | Redis pub/sub + presence (Tide), Temporal workflows + cron (Drift) |
| **Wetland** | `internal/wetland/` | Logging + audit (Footprint), multi-tenancy + RBAC (Marsh), Vault secrets (Canopy), OpenTelemetry (Vapor) |

### Request Flow

```
Client → Load Balancer → Riverbank (chi, port 18789)
          │
          ├─ Global middleware: RealIP → RequestID → Recoverer → CORS
          ├─ Auth middleware: Auth → Tenant → RateLimit
          │
          └─ Burrow 8-stage pipeline
               SessionResolver → WorkspaceLoader → ModelSelector → PromptBuilder
               → ToolPolicyFilter → LLMInvoker → ToolExecutor → SessionPersister
```

### Sandboxing Tiers (Mudbath)

| Tier | Technology | Use Case | Latency |
|------|-----------|----------|---------|
| 1 | WASM via Wazero | Plugins/skills (default) | Microsecond startup |
| 2 | gVisor (runsc) | User tools, medium risk | 20-50% I/O overhead |
| 3 | Firecracker microVM | Untrusted code execution | ~125ms cold start |

## Tech Stack

**Backend:** Go 1.25, chi, pgx v5, go-redis v9, Temporal, Wazero, OpenTelemetry, Cobra, Viper

**Frontend:** Next.js 15, React 19, TypeScript, Zustand, shadcn/ui, Tailwind CSS, next-auth 5

**Infrastructure:** PostgreSQL 16 + pgvector, Redis 7.4, Temporal 1.25, HashiCorp Vault, Grafana + Tempo + Loki

## Quick Start

### Prerequisites

- Go 1.25+
- Node.js 20+
- Docker & Docker Compose
- Make

### Setup

```bash
# Clone the repository
git clone https://github.com/your-org/capyclaw.git
cd capyclaw

# Copy and edit configuration
cp capyclaw.example.yaml capyclaw.yaml

# One-time bootstrap: start infra, run migrations, seed data
./scripts/setup-dev.sh
```

### Development

```bash
# Start backend (Docker infra + hot-reload)
make dev

# Start frontend (separate terminal)
cd web && npm run dev
```

The gateway listens on **port 18789**, the frontend dev server on **port 3000**.

### Build

```bash
make build        # Build gateway binary + capy CLI to bin/
make clean        # Remove binaries
```

## Configuration

CapyClaw uses hierarchical YAML configuration with environment variable overrides:

```bash
# Pattern: CAPYCLAW_<SECTION>_<KEY>
CAPYCLAW_RIVERBANK_PORT=18789
CAPYCLAW_LOG_LEVEL=debug
```

See [`capyclaw.example.yaml`](capyclaw.example.yaml) for all available options. Key defaults:

| Setting | Default |
|---------|---------|
| Gateway port | 18789 |
| Default LLM | `claude-sonnet-4-20250514` |
| Context window | 200k tokens |
| Compaction threshold | 80% |
| pgvector dimensions | 1536 (HNSW, m=16, ef=200) |
| TLS minimum | 1.3 |

## Testing

```bash
make test              # All tests (120s timeout)
make test-unit         # Unit tests only (60s timeout)
make test-integration  # Integration tests (300s, requires running infra)
```

Integration tests live in `tests/integration/` and require PostgreSQL and Redis to be running.

## Code Quality

```bash
make lint    # golangci-lint
make fmt     # gofmt + goimports
make vet     # go vet
```

## Database

```bash
make migrate       # Run migrations up
make migrate-down  # Rollback migrations
make seed          # Seed test data (default tenant + admin user, idempotent)
```

PostgreSQL 16 with pgvector. Core tables include: `tenants`, `users`, `devices`, `agents`, `sessions`, `messages`, `agent_memories` (with vector embeddings), `audit_logs`, `quotas`, `billing`. Row-level security is enforced via `SET app.tenant_id` on each connection.

## Security

CapyClaw was built to address 13+ CVEs found in OpenClaw. Security is foundational, not bolted on:

- **No plaintext credentials** — all secrets managed through HashiCorp Vault
- **No shell expansion** — tool execution uses `exec()` with seccomp-BPF profiles
- **Append-only audit log** — `audit_logs` table entries are never modified
- **Tenant isolation** — tenant context is always propagated; cross-tenant queries are impossible
- **TLS mandatory** — TLS 1.3+ for all external connections in production
- **MCP server trust** — all MCP servers treated as untrusted; tool descriptions sanitized against prompt injection
- **Origin validation** — WebSocket connections enforce Origin header validation (prevents ClawJacked-style attacks)

For detailed threat analysis, CVE breakdown, and architectural rationale, see [`docs/CapyClaw_Research.md`](docs/CapyClaw_Research.md).

## Project Structure

```
capyclaw/
├── cmd/
│   ├── capy/                   # CLI binary
│   └── gateway/                # Gateway server binary
├── internal/
│   ├── riverbank/              # Gateway: HTTP/WS server, middleware, adapters
│   ├── burrow/                 # Agent: runtime, LLM, sandbox, tools, multi-agent
│   ├── pond/                   # Storage: PostgreSQL, pgvector, migrations
│   ├── lodge/                  # Infra: Redis pub/sub, Temporal workflows
│   ├── wetland/                # Platform: logging, RBAC, Vault, OpenTelemetry
│   └── shared/                 # Shared config, errors, types
├── pkg/
│   ├── mcp/                    # Model Context Protocol client
│   ├── protocol/               # Custom wire protocol (JSON-RPC)
│   └── skills/                 # Skill lifecycle management
├── web/                        # Next.js 15 frontend
├── deploy/                     # Docker Compose + Dockerfiles
├── tests/integration/          # Integration tests
├── scripts/                    # Dev scripts (setup, migrate, seed)
├── docs/                       # Documentation
├── capyclaw.example.yaml       # Configuration template
└── Makefile                    # Build, test, lint commands
```

## Documentation

- [`docs/capyclaw-framework-guide.md`](docs/capyclaw-framework-guide.md) — Detailed implementation guide, API endpoints, design decisions
- [`docs/CapyClaw_Research.md`](docs/CapyClaw_Research.md) — Threat analysis, CVE breakdown, architectural rationale

## License

TBD

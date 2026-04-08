# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CapyClaw is a security-first, horizontally-scalable AI agent platform written in Go (backend) + Next.js (frontend). It is a ground-up reimplementation of OpenClaw in Go, designed to address 13+ CVEs and architectural limitations in the original Node.js system.

All internal package names follow a **capybara ecology theme** (a shared team mental model):
- `riverbank` = HTTP/WebSocket gateway
- `burrow` = Agent core runtime
- `pond` = Storage (PostgreSQL + pgvector)
- `lodge` = Infrastructure (Redis, Temporal, Asynq)
- `wetland` = Platform services (observability, RBAC, secrets)

## Commands

### Development

```bash
./scripts/setup-dev.sh    # One-time bootstrap: starts infra, runs migrations, seeds data
make dev                  # Start docker-compose infrastructure + air hot-reload
cd web && npm run dev     # Frontend dev server (port 3000, separate terminal)
```

### Building

```bash
make build        # Build gateway binary + capy CLI to bin/
make clean        # Remove binaries
```

### Testing

```bash
make test              # All tests (120s timeout)
make test-unit         # Unit tests only (60s timeout)
make test-integration  # Integration tests (300s, requires -tags integration)
```

Integration tests live in `tests/integration/` and require running infrastructure (postgres, redis).

### Code Quality

```bash
make lint    # golangci-lint
make fmt     # gofmt + goimports
make vet     # go vet
```

### Database

```bash
make migrate       # Run migrations up
make migrate-down  # Rollback migrations
make seed          # Seed test data (default tenant + admin user, idempotent)
```

## Architecture

### Request Flow

1. Requests enter via `internal/riverbank` (chi router, port 18789)
2. Global middleware: RealIP → RequestID → Recoverer → CORS
3. Authenticated routes: Auth → Tenant → RateLimit middleware
4. Agent messages are processed through an **8-stage pipeline** (`internal/burrow/pipeline.go`):
   - SessionResolver → WorkspaceLoader → ModelSelector → PromptBuilder → ToolPolicyFilter → LLMInvoker → ToolExecutor → SessionPersister

### Key Internal Modules

| Module | Path | Responsibility |
|--------|------|----------------|
| riverbank | `internal/riverbank/` | HTTP/WebSocket server, routing, middleware, channel adapters (Telegram/Discord/Slack) |
| burrow | `internal/burrow/` | Agent runtime, LLM providers (`llm/`), sandboxing (`mudbath/`), tool execution (`nibble/`), multi-agent coordination (`herd/`), prompt engineering (`instinct/`) |
| pond | `internal/pond/` | PostgreSQL + pgvector storage, migrations, three memory types (episodic/semantic/procedural), vector search (`ripple/`) |
| lodge | `internal/lodge/` | Redis pub/sub + presence (`tide/`), Temporal workflows + cron (`drift/`) |
| wetland | `internal/wetland/` | Structured logging + audit (`footprint/`), multi-tenancy + RBAC + quotas (`marsh/`), Vault secrets (`canopy/`), OpenTelemetry (`vapor/`) |

### Public Packages

- `pkg/mcp/` — Model Context Protocol client (transport, tool definitions)
- `pkg/protocol/` — Custom wire protocol (JSON-RPC framing)
- `pkg/skills/` — Skill lifecycle (loading, validation, manifests)

### Configuration

Config is hierarchical YAML (`capyclaw.example.yaml`) with env var overrides using the pattern `CAPYCLAW_<SECTION>_<KEY>`. Key defaults:
- Gateway listens on port **18789**
- Default model: `claude-sonnet-4-20250514`
- Context window: 200k tokens
- Compaction: async at 80% threshold
- pgvector: 1536-dimensional HNSW (m=16, ef_construction=200)

The config struct is in `internal/shared/config/config.go`.

### Database Schema

PostgreSQL 16 with pgvector. Core tables: `tenants`, `users`, `devices`, `agents`, `sessions`, `messages`, `agent_memories` (with vector embeddings), `audit_logs`, `quotas`, `billing`. Row-level security is enforced via `SET app.tenant_id` on each connection.

### Sandboxing Tiers (mudbath)

Tool execution uses risk-based isolation:
- **Tier 1**: WASM via Wazero (default, microsecond startup)
- **Tier 2**: gVisor (syscall interception)
- **Tier 3**: Firecracker microVM (hardware KVM, ~125ms cold start)

### Frontend (web/)

Next.js 15 App Router + React 19 + TypeScript. Key env vars:
- `NEXT_PUBLIC_GATEWAY_WS_URL` (default: `ws://localhost:18789`)
- `NEXT_PUBLIC_GATEWAY_HTTP_URL` (default: `http://localhost:18789`)

State management: Zustand. UI: shadcn/ui + Tailwind CSS. Auth: next-auth 5 (OIDC).

### Infrastructure (deploy/)

Docker Compose services: postgres:16 (pgvector), redis:7.4, temporal:1.25 + temporal-ui, vault:1.17 (dev mode), gateway, web.

## Security Constraints

- **No plaintext credentials** — secrets go through Vault (`internal/wetland/canopy/`)
- **No shell expansion** — tool execution uses `exec()` only with seccomp-BPF profiles
- **Append-only audit log** — never modify `audit_logs` table entries
- **Tenant isolation** — always propagate tenant context; never query across tenants
- **TLS mandatory** — TLS 1.3+ for all external connections in production

## Project Status

Version 0.1.0-alpha. Core scaffolding, configuration, database schema, and middleware are implemented. Pipeline stages, LLM provider integrations, and UI are in progress (many TODO stubs exist). CapyHub skill marketplace and device pairing UI are not yet implemented.

## Key Documentation

- `docs/CapyClaw_Research.md` — Threat analysis, CVE breakdown, architectural rationale
- `docs/capyclaw-framework-guide.md` — Detailed implementation guide, API endpoints, full design decisions

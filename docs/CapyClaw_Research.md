# CapyClaw: a security-first Golang rewrite of OpenClaw

OpenClaw — the fastest-growing open-source project in GitHub history with **327,000+ stars** in 60 days — has accumulated **13+ CVEs** (up to CVSS 9.9), **135,000+ exposed instances** without authentication, and a supply chain attack that poisoned **20% of its plugin marketplace**. Its Node.js architecture imposes hard ceilings on concurrency, persistence, and horizontal scaling that no amount of patching can fix. CapyClaw is a ground-up Golang + React/Next.js reimplementation designed to eliminate every structural weakness while preserving OpenClaw's channel adapter ecosystem and MCP tool compatibility. The core thesis: **security, multi-tenancy, and horizontal scaling must be foundational, not bolted on**.

This plan maps every documented OpenClaw vulnerability and architectural limitation to a specific CapyClaw design decision, backed by concrete Go library selections and implementation patterns.

---

## OpenClaw's security crisis runs deeper than individual CVEs

OpenClaw's security posture is not a case of isolated bugs — it reflects architectural decisions that made exploitation inevitable at scale. Between January and March 2026, researchers disclosed **20+ GitHub Security Advisories** spanning remote code execution, sandbox escapes, authentication bypasses, and supply chain poisoning. Understanding the root causes is essential before designing CapyClaw's countermeasures.

**CVE-2026-25253 (CVSS 8.8)** demonstrated the most dangerous pattern: the Control UI accepted `gatewayUrl` from URL query parameters and auto-connected via WebSocket, transmitting the auth token to any server — including attacker-controlled ones. The WebSocket server failed to validate Origin headers, enabling cross-site hijacking. A single click gave attackers full RCE, even on localhost-bound instances, because the browser initiated the outbound connection. SecurityScorecard's STRIKE team confirmed **12,812 instances** were directly exploitable via this vector.

**CVE-2026-28363 (CVSS 9.9)** — the highest-severity finding — exposed a fundamental flaw in the `tools.exec.safeBins` allowlist. The `sort` binary could be weaponized via GNU long-option abbreviation (`--compress-prog=sh`), bypassing approval gates entirely. **CVE-2026-32025 ("ClawJacked")** showed that localhost WebSocket connections were exempt from rate limiting, allowing malicious JavaScript on any webpage to brute-force the gateway password silently. GitHub issue **#20683** documented how `gateway.controlUi.allowInsecureAuth: true` permitted token-only authentication over unencrypted HTTP without device verification.

The **ClawHavoc campaign** turned OpenClaw's ClawHub marketplace into a malware distribution network. Koi Security's initial audit found **341 malicious skills** out of 2,857 (12%). Expanded scanning revealed **1,184 historically malicious skills** — the primary payload being Atomic macOS Stealer (AMOS), sold as malware-as-a-service for $500–$1,000/month. Skills modified `SOUL.md` and `MEMORY.md` for cross-session persistence, and infostealers like Vidar began specifically targeting `~/.openclaw` directories. Meanwhile, plaintext credential storage in `openclaw.json` meant that **every compromised instance exposed API keys** for Claude, OpenAI, Google AI, and other services.

The MCP ecosystem compounds these risks. Among **2,614 MCP implementations** surveyed, **82% were vulnerable to path traversal**, 67% to code injection, and 34% to command injection. CVE-2025-6514 (CVSS 9.6) in the `mcp-remote` npm package enabled arbitrary OS command execution when clients connected to untrusted servers. Only **8.5% of MCP servers use OAuth** — 53% rely on static API keys.

### CapyClaw security architecture

CapyClaw's security model addresses each root cause with defense-in-depth:

**Authentication and authorization.** Every connection requires mutual TLS or OAuth 2.0/OIDC — there is no "insecure auth" toggle. Device pairing uses ECDH key exchange with SPIFFE identity verification. The Control UI never accepts connection parameters from URL query strings. WebSocket connections enforce Origin header validation, TLS 1.3 minimum, and per-connection rate limiting regardless of source IP. Implementation: `github.com/golang-jwt/jwt/v5` for tokens, `github.com/coreos/go-oidc/v3` for OIDC, Apache Casbin (`github.com/apache/casbin`) for RBAC/ABAC with PostgreSQL-backed policy storage.

**Secret management.** CapyClaw never stores credentials in plaintext config files. All secrets are stored in HashiCorp Vault (`github.com/hashicorp/vault/api`) or encrypted at rest using SOPS (`github.com/getsops/sops/v3`). API keys for LLM providers are injected as short-lived Vault dynamic secrets with automatic rotation. The config file (`capyclaw.yaml`) contains only Vault paths, never raw credentials. At runtime, secrets live only in memory and are zeroed after use via `crypto/subtle.ConstantTimeCompare` patterns.

**Sandbox execution.** CapyClaw implements a three-tier isolation model based on risk level:

- **Tier 1 (plugins/skills)**: WebAssembly sandboxing via Wazero (`github.com/tetratelabs/wazero`), a pure-Go WASM runtime with zero CGO dependencies. Plugins run in memory-isolated WASM modules with no filesystem or network access by default. Microsecond startup, 10ms p50 / 30ms p99 in production (per Arcjet benchmarks). Capabilities are granted explicitly via a host function allowlist
- **Tier 2 (user tools, medium risk)**: gVisor containers (`runsc`) providing syscall interception through a user-space kernel. Integrates natively with Kubernetes via `runtimeClass`. Approximately 20–50% I/O overhead, minimal CPU overhead
- **Tier 3 (untrusted code execution)**: Firecracker microVMs via `github.com/firecracker-microvm/firecracker-go-sdk`. Full KVM hardware virtualization with **~125ms cold start** and **5 MiB memory** per VM. This is what powers AWS Lambda — battle-tested at trillions of invocations

**MCP server trust.** CapyClaw treats every MCP server as untrusted by default. Tool descriptions are sanitized to prevent prompt injection. All MCP connections require mutual TLS. Tool calls are logged to an append-only audit trail before execution. A capability-based permission model restricts which tools each agent can invoke, with per-tool resource quotas enforced at the gateway level.

**Command execution allowlists.** Instead of OpenClaw's string-matching `safeBins` (trivially bypassed via option abbreviation), CapyClaw uses a seccomp-BPF profile that restricts available syscalls per binary. Approved binaries are executed in isolated namespaces with read-only root filesystems. No shell expansion occurs — commands are exec'd directly via `syscall.Exec` with argument arrays, eliminating injection via shell metacharacters.

---

## Performance: from single-threaded ceiling to goroutine-per-session

OpenClaw's Node.js architecture creates three critical performance bottlenecks that cannot be resolved without a rewrite: single-threaded CPU contention during context assembly, JSONL session files that grow unbounded (documented at **5.27 GB** for a single session in GitHub issue #18905), and sqlite-vec's brute-force-only vector search with file-level write locking.

**The single-thread problem is fundamental.** While Node.js handles LLM API streaming well (it is I/O-bound), the CPU-bound work between API calls — JSON parsing, token counting, context compaction, message assembly — blocks the event loop for all concurrent sessions. A single session loading a 400MB JSONL file blocks every other user. Worker Threads and Cluster mode are workarounds, not solutions: Worker Threads add IPC serialization overhead, and Cluster mode requires external state management (Redis) that defeats the file-first design.

**Go eliminates this bottleneck structurally.** Each agent session runs in its own goroutine with a **2–4 KB stack** (growing as needed), sharing a single OS thread pool managed by Go's scheduler. CPU-bound token counting and context assembly happen concurrently across goroutines without blocking. Benchmark data supports this: Scaledrone migrated from Node.js to Go and measured **3x lower memory usage** and significant latency improvements. For WebSocket connections specifically, Go achieves **~2.6 KB per connection** (with gobwas/ws) versus **50–100 KB** in Node.js.

**JSONL files are replaced entirely by PostgreSQL.** Session state moves from append-only local files to structured database tables with proper indexing. The 5.27 GB bloat problem (caused by progress entries duplicating full context) disappears because CapyClaw stores deduplicated message content with foreign key references. Session replay becomes a simple indexed query instead of parsing millions of JSONL lines sequentially.

### Vector search: pgvector replaces sqlite-vec

sqlite-vec uses **brute-force KNN search** (DiskANN is announced but not yet shipped), operates under SQLite's single-writer file lock, and offers no hybrid search capability. pgvector with the pgvectorscale extension achieves **471 QPS at 99% recall on 50M vectors** — 11.4x better than Qdrant's 41 QPS at equivalent recall.

CapyClaw's memory schema leverages pgvector's HNSW indexing with hybrid search (vector + full-text via Reciprocal Rank Fusion). The recommended schema:

```sql
CREATE TABLE agent_memories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    content TEXT NOT NULL,
    embedding vector(1536),
    memory_type VARCHAR(50), -- 'episodic', 'semantic', 'procedural'
    metadata JSONB DEFAULT '{}',
    importance_score FLOAT DEFAULT 0.5,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_memories_hnsw ON agent_memories
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 200);
CREATE INDEX idx_memories_fts ON agent_memories
    USING GIN (to_tsvector('english', content));
```

Go integration uses **pgx v5** (`github.com/jackc/pgx/v5`) with `pgvector-go/pgx` for native vector type support, connection pooling via `pgxpool`, and binary protocol for maximum throughput. HNSW parameters tuned for production: `m = 16`, `ef_construction = 200`, runtime `hnsw.ef_search = 100` for >95% recall.

### Context window management without 30-second freezes

OpenClaw's synchronous in-band compaction pauses the agent for **30–60 seconds** while sending the entire context to an LLM for summarization. CapyClaw replaces this with asynchronous background compaction:

- **Rolling summaries** are maintained in a separate goroutine. When token count approaches 80% of the model's context window, the compaction goroutine summarizes older messages using a fast model (Gemini Flash at ~$0.006 per 80K-token compaction) while the agent continues operating on the current context
- **Tool result clearing** removes raw tool output once the agent has processed it — a lossless optimization that typically recovers 40–60% of context space
- **Three-layer memory** (in-context summary + pgvector semantic memory + PostgreSQL full archive) achieves **~100% entity retention** compared to compaction-only approaches that retain only **23%**

---

## Reliability through horizontal scaling and shared-nothing compute

OpenClaw's single-instance architecture is its most limiting design constraint. With session state on local disk, SQLite's file-level locking, and no distributed scheduling, there is no path to high availability or horizontal scaling without fundamental changes.

CapyClaw's architecture is **stateless-compute with externalized state**:

```
        ┌──────────────────────┐
        │   Load Balancer      │
        │  (L7, sticky WS)     │
        └──────────┬───────────┘
                   │
     ┌─────────────┼─────────────┐
     │             │             │
 ┌───┴───┐   ┌────┴────┐  ┌────┴────┐
 │CapyClaw│   │CapyClaw │  │CapyClaw │
 │  N=1   │   │  N=2    │  │  N=3    │
 │(Go bin)│   │(Go bin) │  │(Go bin) │
 └───┬────┘   └────┬────┘  └────┬────┘
     │             │             │
 ┌───┴─────────────┴─────────────┴───┐
 │          Redis Cluster             │
 │ (session state, pub/sub, Asynq)   │
 └──────────────────┬────────────────┘
                    │
 ┌──────────────────┴────────────────┐
 │    PostgreSQL + pgvector (HA)     │
 │ (memory, conversations, tenants)  │
 └───────────────────────────────────┘
```

**Session state** lives in Redis, not on disk. Any CapyClaw instance can resume any session. WebSocket affinity at the load balancer routes reconnections to the same instance when possible, but Redis pub/sub (`github.com/redis/go-redis/v9`) enables cross-instance message delivery when sessions migrate.

**Distributed task scheduling** replaces OpenClaw's single-instance cron. CapyClaw uses Asynq (`github.com/hibiken/asynq`) for simple async jobs (embedding generation, batch exports) and Temporal (`go.temporal.io/sdk`) for complex multi-step agent workflows requiring durable execution guarantees. Temporal's saga pattern handles multi-agent coordination with automatic retry and compensation logic.

**Graceful shutdown** in Go is idiomatic and robust. On SIGTERM, CapyClaw drains active WebSocket connections (sending close code 1001), completes in-flight LLM requests within a configurable timeout (default 30s), flushes pending writes to PostgreSQL, and deregisters from the load balancer. The `context.Context` cancellation pattern propagates shutdown signals cleanly through all goroutines.

**Health checking and circuit breaking** use `github.com/sony/gobreaker/v2` for LLM provider failover (automatic circuit-open after configurable failure thresholds) and standard Kubernetes liveness/readiness probes. OpenTelemetry (`go.opentelemetry.io/otel`) provides distributed tracing across agent sessions, LLM calls, tool executions, and database queries — the observability gap that enterprises consistently cite as a blocker.

---

## Multi-tenancy, RBAC, and audit logging fill enterprise gaps

Fewer than **10% of enterprises** have AI agents running in full production (Gartner), and **88%** experienced a confirmed or suspected AI agent security incident in the past year (Teleport 2026). The primary blockers are not agent capability but governance: multi-tenancy, access control, audit trails, and quota management. OpenClaw is explicitly **single-user by design**.

CapyClaw implements **namespace-isolated multi-tenancy**: each tenant gets a logically isolated PostgreSQL schema (or row-level security with `tenant_id` columns), separate Redis key prefixes, and independent resource quotas. This hybrid approach balances isolation with operational efficiency — no per-tenant infrastructure, but strong data separation enforced at the database layer.

**RBAC with Apache Casbin** supports tenant-scoped roles (admin, operator, viewer, agent), per-tool permission grants, and attribute-based policies for dynamic authorization (an agent's effective permissions can change mid-session based on the tools it invokes). Policies are stored in PostgreSQL and cached in memory with configurable TTL.

**Audit logging** writes to an append-only PostgreSQL table with the following guarantees: every tool invocation, LLM API call, configuration change, and authentication event is logged with actor identity, timestamp, tenant context, and request/response hashes. Logs are immutable (INSERT-only, no UPDATE or DELETE grants). For compliance-sensitive deployments, logs can stream to an external SIEM via OpenTelemetry's log exporter.

**Rate limiting and quota management** use `golang.org/x/time/rate` for per-tenant token bucket limiting at the gateway, with Redis-backed distributed counters for cross-instance consistency. Configurable per-tenant budgets cap monthly LLM spend, with alerts at 80% and hard cutoffs at 100%. Per-tenant concurrency limits prevent noisy-neighbor resource exhaustion.

---

## Go library selections and the CapyClaw technology stack

The complete stack is chosen for production maturity, active maintenance, and minimal dependency surface:

| Component | Library | Rationale |
|---|---|---|
| LLM abstraction | `github.com/tmc/langchaingo` v0.1.13+ | Most mature Go LLM framework, 6,500+ stars, 839+ importers, built-in pgvector integration |
| MCP protocol | `github.com/modelcontextprotocol/go-sdk` | Official SDK, maintained with Google, full spec support including OAuth |
| WebSocket | `github.com/coder/websocket` | Idiomatic Go, context.Context support, concurrent writes, maintained by Coder |
| PostgreSQL | `github.com/jackc/pgx/v5` + `pgvector-go` | Fastest Go PG driver, native binary protocol, connection pooling via pgxpool |
| RBAC | `github.com/apache/casbin` v2 | ACL/RBAC/ABAC, tenant isolation, PostgreSQL adapter, Fortune 500 adoption |
| JWT/OIDC | `github.com/golang-jwt/jwt/v5` + `go-oidc/v3` | Industry standard, RS256/ES256/EdDSA |
| Task queue | `github.com/hibiken/asynq` | Redis-backed, priority queues, scheduling, web UI |
| Workflows | `go.temporal.io/sdk` v1 | Durable execution for multi-agent coordination |
| WASM sandbox | `github.com/tetratelabs/wazero` v1 | Pure Go, zero CGO, AOT compilation |
| MicroVM | `firecracker-go-sdk` | Full hardware isolation for untrusted code |
| Observability | `go.opentelemetry.io/otel` v1 | Traces, metrics, logs — vendor-neutral |
| Rate limiting | `golang.org/x/time/rate` + `ulule/limiter` | Token bucket + Redis distributed limiting |
| Validation | `go-playground/validator/v10` | Struct tag validation, custom rules |
| Circuit breaker | `github.com/sony/gobreaker/v2` | LLM provider failover |

The frontend remains **React/Next.js** (matching OpenClaw's Control UI technology) with a hardened API gateway. All Control UI communications use the same OAuth/OIDC flow as programmatic access — no separate "insecure" auth path exists.

---

## Conclusion

CapyClaw's design is not a theoretical exercise — it is a direct response to **512 documented vulnerabilities**, an architecture that cannot scale beyond a single process, and an enterprise readiness gap that leaves 90% of AI agent deployments failing within 30 days. The three decisions that matter most are replacing file-based persistence with PostgreSQL + pgvector (eliminating the entire class of JSONL corruption, bloat, and locking issues), implementing tiered sandboxing via Wazero/gVisor/Firecracker (making the ClawHavoc-style supply chain attack structurally impossible at the code execution layer), and building multi-tenancy as a first-class database-level concern rather than an application-layer afterthought.

Go's goroutine model delivers **3x memory efficiency** over Node.js for WebSocket workloads and eliminates single-threaded CPU contention during context assembly. pgvector with HNSW achieves **471 QPS at 99% recall on 50M vectors**, replacing sqlite-vec's brute-force search. Temporal provides durable workflow execution for multi-agent orchestration that OpenClaw's single-instance cron cannot match. Every library in the stack is production-proven, actively maintained, and selected to minimize dependency surface area — because the next ClawHavoc will target the supply chain of whatever replaces OpenClaw.
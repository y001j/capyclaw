# Phase 1: 基础加固 — 认证、错误处理、依赖注入

## 目标

让 Gateway 能真正启动并连接所有基础设施（PostgreSQL、Redis），实现真实的 JWT 认证、速率限制、RBAC 权限检查，以及统一的错误处理框架。后续所有阶段的 Handler 实现都依赖本阶段完成的依赖注入体系。

## 前置条件

- 无（起始阶段）
- 基础设施（PostgreSQL 16 + pgvector、Redis 7.4）通过 `docker compose up` 启动
- `make migrate && make seed` 完成数据库初始化

## 当前状态

### 已实现
- `config.Load()` 完整配置加载（`internal/shared/config/config.go`）
- `telemetry.Init()` OTEL 初始化（`internal/shared/telemetry/init.go`）
- `pond.New()` 数据库连接池（`internal/pond/db.go`）
- CORS 中间件（`internal/riverbank/middleware/cors.go`）— 100%
- Tenant 中间件（`internal/riverbank/middleware/tenant.go`）— 100%
- RequestID 中间件（`internal/riverbank/middleware/requestid.go`）— 100%
- RBAC 管理器（`internal/wetland/marsh/rbac.go`）— Casbin 集成已完成
- 审计日志器（`internal/wetland/footprint/logger.go`）— Log() 方法已实现

### 需要实现
- Server 目前仅持有 `*config.Config`，无 DB/Redis 等依赖注入
- JWT 验证返回空 claims（`internal/riverbank/middleware/auth.go:65-68`）
- OIDC 和 device-pairing 认证路径为 TODO
- 速率限制直接放行（`internal/riverbank/middleware/ratelimit.go:18-22`）
- 健康检查不检测实际连接（`internal/riverbank/handlers.go:14-17`）
- 无统一错误处理框架

---

## 实施任务

### 1. 统一错误处理框架

**新建文件**: `internal/shared/errors/errors.go`

```go
// 核心错误类型
type AppError struct {
    Code    string // "not_found", "unauthorized", "rate_limited", "conflict", "internal"
    Message string
    Err     error  // 原始错误（不暴露给客户端）
}

// 预定义错误
var (
    ErrNotFound      = &AppError{Code: "not_found", Message: "resource not found"}
    ErrUnauthorized  = &AppError{Code: "unauthorized", Message: "authentication required"}
    ErrForbidden     = &AppError{Code: "forbidden", Message: "insufficient permissions"}
    ErrRateLimited   = &AppError{Code: "rate_limited", Message: "rate limit exceeded"}
    ErrConflict      = &AppError{Code: "conflict", Message: "resource already exists"}
    ErrBadRequest    = &AppError{Code: "bad_request", Message: "invalid request"}
)

// HTTP 状态码映射
func HTTPStatus(err *AppError) int

// pgx 错误转换
func FromPgxError(err error) *AppError

// 统一 JSON 错误响应
func WriteError(w http.ResponseWriter, err *AppError)
```

**实现要点**：
- `FromPgxError` 映射 `pgx.ErrNoRows` → `ErrNotFound`，unique constraint 违反 → `ErrConflict`
- `WriteError` 输出格式：`{"error": {"code": "...", "message": "..."}}`
- 生产环境不暴露内部错误详情

### 2. Server 依赖注入重构

**修改文件**: `internal/riverbank/server.go`

当前 Server 结构体只有 `cfg` 和 `router`，需要添加所有服务依赖：

```go
type Server struct {
    cfg    *config.Config
    router http.Handler
    http   *http.Server

    // 数据层
    db          *pond.DB
    agentRepo   *pebble.AgentRepository
    sessionRepo *pebble.SessionRepository
    messageRepo *pebble.MessageRepository

    // 平台服务
    tenantMgr   *marsh.TenantManager
    quotaMgr    *marsh.QuotaManager
    auditLogger *footprint.Logger

    // 基础设施
    redis     *redis.Client
    scheduler *drift.Scheduler
}
```

**修改 `NewServer`**：
1. 创建 `pond.New(ctx, cfg.Pond.Postgres)` 获取 DB pool
2. 创建 Redis client: `redis.NewClient(&redis.Options{Addr: cfg.Lodge.Redis.Addresses[0]})`
3. 初始化所有 Repository: `pebble.NewAgentRepository(db.Pool)` 等
4. 初始化 `marsh.NewTenantManager(db.Pool)`, `marsh.NewQuotaManager(db.Pool)`
5. 初始化 `footprint.NewLogger(db.Pool)`
6. 初始化 `drift.NewScheduler(cfg.Lodge.Asynq.RedisAddr)`

**修改 `Shutdown`**：
- 关闭 DB pool: `s.db.Close()`
- 关闭 Redis: `s.redis.Close()`
- 关闭 Scheduler: `s.scheduler.Close()`

**修改 `cmd/gateway/main.go`**：
- `NewServer` 签名变为 `NewServer(ctx context.Context, cfg *config.Config)` 以支持 DB 初始化
- 将 DB、Redis 初始化移入 NewServer 或在 main 中创建后传入

### 3. JWT 认证实现

**修改文件**: `internal/riverbank/middleware/auth.go`

当前 `validateJWT` 返回空 claims，需要真实实现：

```go
func validateJWT(tokenStr string, cfg config.JWTConfig) (*JWTClaims, error) {
    // 1. 从 cfg.PublicKeyPath 加载公钥（支持 PEM 格式）
    // 2. 根据 cfg.SigningMethod 选择签名方法：
    //    - "ES256" → jwt.SigningMethodES256
    //    - "RS256" → jwt.SigningMethodRS256
    //    - "EdDSA" → jwt.SigningMethodEdDSA
    // 3. jwt.Parse(tokenStr, keyFunc) 验证签名和过期时间
    // 4. 提取 claims: sub → UserID, tid → TenantID, role → Role
}
```

**关键安全考虑**：
- KeyFunc 中严格验证 `alg` 头与配置一致，防止算法混淆攻击
- 检查 `exp`、`iat`、`nbf` 标准声明
- 使用 `golang-jwt/jwt/v5`（已在 go.mod 中）

**公钥加载**（新增函数）：
```go
func loadPublicKey(path string, method string) (any, error)
// 根据 method 解析为 *ecdsa.PublicKey, *rsa.PublicKey, 或 ed25519.PublicKey
```

**开发模式**：
- 当 `cfg.PublicKeyPath` 为空时，生成临时密钥对用于开发（仅限 log_level=debug）
- 生产环境强制要求公钥路径

### 4. 速率限制实现

**修改文件**: `internal/riverbank/middleware/ratelimit.go`

当前直接调用 `next.ServeHTTP`，需要基于 Redis 的分布式限流：

```go
func RateLimit(cfg config.RapidsConfig, redisClient *redis.Client) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !cfg.Enabled {
                next.ServeHTTP(w, r)
                return
            }

            // 1. 从 context 获取 tenant_id + user_id
            tenantID := r.Context().Value(middleware.TenantIDKey).(string)
            userID := r.Context().Value(middleware.UserIDKey).(string)

            // 2. Redis 滑动窗口计数
            key := fmt.Sprintf("ratelimit:%s:%s", tenantID, userID)
            // 使用 MULTI/EXEC 原子操作：INCR + EXPIRE

            // 3. 检查 per-minute 和 per-hour 限制
            // 4. 超限返回 429 + Retry-After 头
            // 5. 记录 OTel metric: TenantRateLimitHits
        })
    }
}
```

**注意**: `RateLimit` 签名需要增加 `redisClient` 参数，`setupRouter()` 中传入。

### 5. 健康检查增强

**修改文件**: `internal/riverbank/handlers.go`

```go
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()

    status := map[string]any{"status": "ready"}
    httpStatus := http.StatusOK

    // 检查 PostgreSQL
    if err := s.db.Pool.Ping(ctx); err != nil {
        status["postgres"] = "down"
        httpStatus = http.StatusServiceUnavailable
    } else {
        status["postgres"] = "up"
    }

    // 检查 Redis
    if err := s.redis.Ping(ctx).Err(); err != nil {
        status["redis"] = "down"
        httpStatus = http.StatusServiceUnavailable
    } else {
        status["redis"] = "up"
    }

    if httpStatus != http.StatusOK {
        status["status"] = "not ready"
    }

    writeJSON(w, httpStatus, status)
}

func (s *Server) handleDetailedHealth(w http.ResponseWriter, r *http.Request) {
    // 各组件延迟测量
    // 返回 DB pool 统计信息、Redis info、连接数等
}
```

### 6. RLS 租户隔离集成

**修改文件**: `internal/pond/db.go`

当前 `SetTenantContext` 使用 Pool 级 Exec，这会获取一个连接执行后立即释放，后续查询可能使用不同的连接。需要改为每次请求获取专用连接：

```go
// AcquireWithTenant 获取一个设置了租户上下文的连接
func (db *DB) AcquireWithTenant(ctx context.Context, tenantID string) (*pgxpool.Conn, error) {
    conn, err := db.Pool.Acquire(ctx)
    if err != nil {
        return nil, err
    }
    _, err = conn.Exec(ctx, "SET app.tenant_id = $1", tenantID)
    if err != nil {
        conn.Release()
        return nil, err
    }
    return conn, nil
}
```

**或者**在中间件层面注入：在 Tenant 中间件中获取连接，设置 tenant_id，放入 context，请求结束时释放。

### 7. Router 中间件接线修改

**修改文件**: `internal/riverbank/router.go`

当前中间件构造函数只接受 config，需要传入实际服务实例：

```go
func (s *Server) setupRouter() (http.Handler, error) {
    r := chi.NewRouter()
    // ... 全局中间件不变 ...

    r.Group(func(r chi.Router) {
        r.Use(middleware.Auth(s.cfg.Riverbank.Auth))
        r.Use(middleware.Tenant())
        r.Use(middleware.RateLimit(s.cfg.Rapids, s.redis)) // 新增 redis 参数
        // ... 路由不变 ...
    })
    return r, nil
}
```

### 8. 单元测试

**新建文件**:
- `internal/riverbank/middleware/auth_test.go` — JWT 验证正确性
- `internal/riverbank/middleware/ratelimit_test.go` — 限流逻辑
- `internal/shared/errors/errors_test.go` — 错误映射

---

## 关键文件清单

| 文件 | 操作 |
|------|------|
| `internal/shared/errors/errors.go` | **新建** |
| `internal/riverbank/server.go` | **重构** — 依赖注入 |
| `internal/riverbank/router.go` | **修改** — 传入 Redis |
| `internal/riverbank/handlers.go` | **修改** — 健康检查 |
| `internal/riverbank/middleware/auth.go` | **实现** — JWT 验证 |
| `internal/riverbank/middleware/ratelimit.go` | **实现** — Redis 限流 |
| `internal/pond/db.go` | **增强** — 租户连接获取 |
| `cmd/gateway/main.go` | **修改** — 适配新 NewServer 签名 |

## 可复用的已有代码

- `marsh.RBACManager.CheckPermission()` — RBAC 检查已完整实现
- `footprint.Logger.Log()` — 审计日志写入已实现
- `marsh.TenantManager.Get()` — 租户查询已实现
- `pond.DB.SetTenantContext()` — RLS 基础已实现
- `config.AuthConfig`, `config.JWTConfig` — 配置结构已完整定义

## 验收标准

1. **Gateway 启动连接检查**:
   - `curl http://localhost:18789/healthz` → `{"status":"ok"}`
   - `curl http://localhost:18789/readyz` → `{"status":"ready","postgres":"up","redis":"up"}`
   - PostgreSQL 停止后 → `{"status":"not ready","postgres":"down","redis":"up"}`

2. **认证**:
   - 无 Bearer token → `401 {"error":{"code":"unauthorized"}}`
   - 过期 JWT → `401`
   - 有效 JWT → 通过认证，context 包含 user_id/tenant_id/role

3. **速率限制**:
   - 超过 requests_per_minute (默认 60) → `429 + Retry-After`

4. **RBAC**:
   - 非 admin 角色访问 `/api/v1/admin/*` → `403`

5. **租户隔离**:
   - Tenant A 创建的 Agent，Tenant B 无法通过 API 查到

## 验证方法

```bash
# 1. 启动基础设施
make docker-up
make migrate && make seed

# 2. 生成测试 JWT（需要写一个简单脚本或用 jwt.io）
# 使用 ES256 签名，claims: {"sub":"user-id","tid":"tenant-id","role":"admin","exp":...}

# 3. 启动 Gateway
make dev

# 4. 测试健康检查
curl http://localhost:18789/healthz
curl http://localhost:18789/readyz

# 5. 测试认证
curl -H "Authorization: Bearer <valid-token>" http://localhost:18789/api/v1/agents
curl http://localhost:18789/api/v1/agents  # 预期 401

# 6. 运行单元测试
make test-unit
```

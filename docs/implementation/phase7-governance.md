# Phase 7: 审计、配额、计费、密钥管理、管理 API

## 目标

完成治理功能：全面审计日志查询与导出、配额执行阻止超预算使用、计费跟踪、Vault 密钥轮换、以及完整的管理 API。

## 前置条件

- Phase 1 完成（认证、RBAC、审计日志基础）
- Phase 2 完成更佳（Agent CRUD、对话循环）

## 当前状态

### 已实现
- `footprint.Logger.Log()`（`internal/wetland/footprint/logger.go`）— 审计事件写入 ✓
- `footprint.Logger.Query()` — 返回 nil，TODO
- `marsh.TenantManager` — Get/Create/List ✓，缺 Update
- `marsh.RBACManager` — CheckPermission/AddRole/RemoveRole/GetRoles 全部完成 ✓
- `marsh.QuotaManager` — CheckQuota/RecordUsage 结构体和签名 ✓，实现为 TODO
- `marsh.BillingTracker` — RecordLLMUsage 结构体 ✓，实现为 TODO
- `canopy.VaultClient` — GetSecret/ResolveRef 已实现 ✓
- `canopy.SecretRotator` — Start/rotate 框架 ✓，rotate 为 TODO
- `canopy.SOPSClient` — GetSecret 返回 "not implemented"
- DB schema: `audit_log` 表 + 索引 ✓
- DB schema: `tenants.quota` JSONB 字段 ✓

### 需要实现
- 审计日志查询过滤 + 分页
- 审计事件类型定义
- 审计中间件（自动记录 API 请求）
- 审计日志导出器
- 配额检查实际查询（聚合 sessions 表用量）
- 用量记录到数据库
- 计费报告生成
- 密钥轮换逻辑
- SOPS 解密
- 管理 API Handler
- 补充 DB 迁移

---

## 实施任务

### 1. 审计事件类型

**新建文件**: `internal/wetland/footprint/events.go`

```go
// AuditEvent 审计事件结构
type AuditEvent struct {
    TenantID     uuid.UUID       `json:"tenant_id"`
    ActorID      uuid.UUID       `json:"actor_id"`
    ActorType    string          `json:"actor_type"` // "user", "agent", "system"
    Action       string          `json:"action"`
    ResourceType string          `json:"resource_type"`
    ResourceID   uuid.UUID       `json:"resource_id"`
    Details      json.RawMessage `json:"details"`
    IPAddress    string          `json:"ip_address"`
    UserAgent    string          `json:"user_agent"`
    RequestID    string          `json:"request_id"`
}

// AuditFilter 审计查询过滤器
type AuditFilter struct {
    TenantID     *uuid.UUID
    ActorID      *uuid.UUID
    Action       string
    ResourceType string
    StartTime    *time.Time
    EndTime      *time.Time
    Limit        int
    Cursor       int64 // 基于 audit_log.id 的游标分页
}

// 预定义审计动作
const (
    ActionAgentCreate   = "agent.create"
    ActionAgentUpdate   = "agent.update"
    ActionAgentDelete   = "agent.delete"
    ActionSessionCreate = "session.create"
    ActionSessionArchive = "session.archive"
    ActionSkillInstall  = "skill.install"
    ActionSkillUninstall = "skill.uninstall"
    ActionTenantCreate  = "tenant.create"
    ActionTenantSuspend = "tenant.suspend"
    ActionTenantUpdate  = "tenant.update"
    ActionAuthLogin     = "auth.login"
    ActionAuthFailed    = "auth.failed"
    ActionQuotaExceeded = "quota.exceeded"
    ActionSecretRotated = "secret.rotated"
)
```

### 2. 审计日志查询

**修改文件**: `internal/wetland/footprint/logger.go`

```go
func (l *Logger) Query(ctx context.Context, filter AuditFilter) ([]*AuditEvent, error) {
    query := `
        SELECT id, tenant_id, actor_id, actor_type, action,
               resource_type, resource_id, details,
               ip_address, user_agent, request_id, created_at
        FROM audit_log
        WHERE 1=1
    `
    args := []any{}
    argIdx := 1

    if filter.TenantID != nil {
        query += fmt.Sprintf(" AND tenant_id = $%d", argIdx)
        args = append(args, *filter.TenantID)
        argIdx++
    }
    if filter.Action != "" {
        query += fmt.Sprintf(" AND action = $%d", argIdx)
        args = append(args, filter.Action)
        argIdx++
    }
    // ... StartTime, EndTime, ActorID, ResourceType 类似

    if filter.Cursor > 0 {
        query += fmt.Sprintf(" AND id < $%d", argIdx)
        args = append(args, filter.Cursor)
        argIdx++
    }

    limit := filter.Limit
    if limit <= 0 { limit = 50 }
    query += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", argIdx)
    args = append(args, limit)

    rows, err := l.pool.Query(ctx, query, args...)
    // scan...
}
```

### 3. 审计中间件

**新建文件**: `internal/riverbank/middleware/audit.go`

```go
func Audit(logger *footprint.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 用 ResponseWriter wrapper 捕获状态码
            rw := &responseWriter{ResponseWriter: w}
            next.ServeHTTP(rw, r)

            // 异步记录审计日志（不阻塞响应）
            go func() {
                // 只记录写操作和关键读操作
                if r.Method == "GET" && !isAuditableGet(r.URL.Path) {
                    return
                }
                logger.Log(context.Background(), &footprint.AuditEvent{
                    TenantID:  extractTenantID(r),
                    ActorID:   extractUserID(r),
                    ActorType: "user",
                    Action:    deriveAction(r.Method, r.URL.Path),
                    IPAddress: r.RemoteAddr,
                    UserAgent: r.UserAgent(),
                    RequestID: r.Header.Get("X-Request-Id"),
                })
            }()
        })
    }
}
```

### 4. 审计导出器

**新建文件**: `internal/wetland/footprint/exporter.go`

```go
type Exporter struct {
    logger   *Logger
    endpoint string
    interval time.Duration
    lastID   int64
}

func (e *Exporter) Start(ctx context.Context) {
    ticker := time.NewTicker(e.interval)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            e.export(ctx)
        }
    }
}

func (e *Exporter) export(ctx context.Context) {
    // 1. 查询 id > lastID 的审计事件
    // 2. 批量 POST 到 SIEM endpoint
    // 3. 更新 lastID
}
```

### 5. 配额执行

**修改文件**: `internal/wetland/marsh/quota.go`

```go
func (m *QuotaManager) CheckQuota(ctx context.Context, tenantID uuid.UUID, usage UsageRequest) (bool, error) {
    // 1. 获取租户配额配置
    var quota struct {
        MonthlyTokenBudget  int64   `json:"monthly_token_budget"`
        MonthlyCostLimitUSD float64 `json:"monthly_cost_limit_usd"`
    }
    err := m.pool.QueryRow(ctx, `
        SELECT quota FROM tenants WHERE id = $1
    `, tenantID).Scan(&quota)

    // 2. 聚合当月已用量
    var currentUsage struct {
        TotalTokens int64
        TotalCost   float64
    }
    m.pool.QueryRow(ctx, `
        SELECT COALESCE(SUM(total_input_tokens + total_output_tokens), 0),
               COALESCE(SUM(total_cost_usd), 0)
        FROM sessions
        WHERE tenant_id = $1
          AND created_at >= date_trunc('month', NOW())
    `, tenantID).Scan(&currentUsage.TotalTokens, &currentUsage.TotalCost)

    // 3. 检查新用量是否超标
    if currentUsage.TotalTokens + usage.TokensUsed > quota.MonthlyTokenBudget {
        return false, nil
    }
    if currentUsage.TotalCost + usage.CostUSD > quota.MonthlyCostLimitUSD {
        return false, nil
    }

    return true, nil
}

func (m *QuotaManager) RecordUsage(ctx context.Context, tenantID uuid.UUID, tokens int64, costUSD float64) error {
    // sessions 表的 token/cost 已在 SessionPersister 中更新
    // 这里可以更新 usage_monthly 聚合表（如果有）
    return nil
}

// CheckAlert 检查是否需要发送预算告警
func (m *QuotaManager) CheckAlert(ctx context.Context, tenantID uuid.UUID) (bool, float64, error) {
    // 当前用量 / 预算 > alert_threshold (default 0.8) → 需要告警
}
```

**在 Pipeline 中集成配额检查**：在 LLMInvoker 之前（或 Pipeline.Execute 入口处）调用 `CheckQuota`，如果超标返回明确错误。

### 6. 计费跟踪

**修改文件**: `internal/wetland/marsh/billing.go`

```go
func (b *BillingTracker) RecordLLMUsage(ctx context.Context, tenantID uuid.UUID, provider, model string, inputTokens, outputTokens int, costUSD float64) error {
    // 1. 记录到 audit_log（带 details）
    // 2. 检查预算告警阈值
    alert, percentage, _ := b.quotaManager.CheckAlert(ctx, tenantID)
    if alert {
        slog.Warn("tenant approaching budget limit",
            "tenant_id", tenantID,
            "usage_percentage", percentage,
        )
        // 可选：通过 webhook/email 通知
    }
    return nil
}

// GenerateUsageReport 生成指定时间段的用量报告
func (b *BillingTracker) GenerateUsageReport(ctx context.Context, tenantID uuid.UUID, startDate, endDate time.Time) (*UsageReport, error) {
    report := &UsageReport{}
    // 聚合 sessions 表：按模型分组的 token 用量和费用
    rows, _ := b.pool.Query(ctx, `
        SELECT m.model,
               COUNT(DISTINCT s.id) as session_count,
               SUM(s.total_input_tokens) as input_tokens,
               SUM(s.total_output_tokens) as output_tokens,
               SUM(s.total_cost_usd) as cost_usd
        FROM sessions s
        LEFT JOIN messages m ON m.session_id = s.id
        WHERE s.tenant_id = $1
          AND s.created_at BETWEEN $2 AND $3
        GROUP BY m.model
    `, tenantID, startDate, endDate)
    // ...
}
```

### 7. 密钥轮换实现

**修改文件**: `internal/wetland/canopy/rotation.go`

```go
func (r *SecretRotator) rotate(ctx context.Context) {
    slog.Info("checking for secret rotation")

    // 1. 列出需要轮换的密钥路径
    paths := []string{
        "capyclaw/llm/anthropic",
        "capyclaw/llm/openai",
    }

    for _, path := range paths {
        // 2. 从 Vault 读取密钥元数据
        metadata, err := r.vault.client.KVv2(r.vault.mountPath).GetMetadata(ctx, path)
        if err != nil { continue }

        // 3. 检查是否到达轮换周期
        if time.Since(metadata.CreatedTime) < r.interval {
            continue
        }

        // 4. 通知依赖服务刷新（通过 callback 或 channel）
        slog.Info("secret rotation needed", "path", path)
        if r.onRotate != nil {
            r.onRotate(path)
        }
    }
}
```

### 8. 管理 API Handler

**修改文件**: `internal/riverbank/handlers.go`

```go
func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
    tenants, err := s.tenantMgr.List(r.Context())
    // 返回 JSON 数组
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Name  string `json:"name"`
        Slug  string `json:"slug"`
        Plan  string `json:"plan"`
    }
    // 创建 + 审计日志
}

func (s *Server) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
    // PATCH: 更新 plan, status, quota
    // 特别处理 status = "suspended" → 暂停所有该租户的 agent
}

func (s *Server) handleTenantUsage(w http.ResponseWriter, r *http.Request) {
    tenantID := chi.URLParam(r, "tenantID")
    // 调用 BillingTracker.GenerateUsageReport()
    // 返回按模型分组的用量报告
}

func (s *Server) handleQueryAudit(w http.ResponseWriter, r *http.Request) {
    // 解析查询参数: ?action=&start=&end=&limit=&cursor=
    filter := footprint.AuditFilter{...}
    events, err := s.auditLogger.Query(r.Context(), filter)
    // 返回 JSON 数组 + 分页游标
}
```

### 9. 补充数据库迁移

**新建文件**: `internal/pond/migrations/002_governance.up.sql`

```sql
-- 月度用量聚合表（可选，优化查询性能）
CREATE TABLE IF NOT EXISTS usage_monthly (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    month DATE NOT NULL,
    model VARCHAR(100),
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cost_usd DECIMAL(12,6) NOT NULL DEFAULT 0,
    session_count INT NOT NULL DEFAULT 0,
    UNIQUE(tenant_id, month, model)
);

-- 租户更新支持
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS suspended_reason TEXT;
```

### 10. 租户暂停逻辑

**修改文件**: `internal/wetland/marsh/tenant.go`

```go
func (m *TenantManager) Update(ctx context.Context, id uuid.UUID, updates map[string]any) (*Tenant, error) {
    // 动态 SQL 构建 PATCH 更新
}

func (m *TenantManager) Suspend(ctx context.Context, id uuid.UUID, reason string) error {
    _, err := m.pool.Exec(ctx, `
        UPDATE tenants SET status = 'suspended', suspended_at = NOW(), suspended_reason = $2
        WHERE id = $1
    `, id, reason)
    return err
}
```

**在认证中间件中检查租户状态**：

```go
// auth.go 中 JWT 验证后
tenant, err := tenantMgr.Get(ctx, tenantID)
if tenant.Status == "suspended" {
    http.Error(w, "tenant suspended", http.StatusForbidden)
    return
}
```

---

## 关键文件清单

| 文件 | 操作 |
|------|------|
| `internal/wetland/footprint/events.go` | **新建** — 事件类型 |
| `internal/wetland/footprint/logger.go` | **实现** — Query 方法 |
| `internal/wetland/footprint/exporter.go` | **新建** — SIEM 导出 |
| `internal/riverbank/middleware/audit.go` | **新建** — 审计中间件 |
| `internal/wetland/marsh/quota.go` | **实现** — CheckQuota/RecordUsage |
| `internal/wetland/marsh/billing.go` | **实现** — RecordLLMUsage + 报告 |
| `internal/wetland/marsh/tenant.go` | **增强** — Update/Suspend |
| `internal/wetland/canopy/rotation.go` | **实现** — rotate 逻辑 |
| `internal/riverbank/handlers.go` | **实现** — Admin API |
| `internal/pond/migrations/002_governance.up.sql` | **新建** |

## 可复用的已有代码

- `footprint.Logger.Log()` — 审计写入（`logger.go:22-33`）
- `marsh.RBACManager.CheckPermission()` — RBAC（`rbac.go:25-27`）
- `marsh.TenantManager.Get/Create/List()` — 租户 CRUD（`tenant.go`）
- `canopy.VaultClient.GetSecret/ResolveRef()` — Vault 客户端（`vault.go`）
- `canopy.SecretRotator.Start()` — 轮换循环框架（`rotation.go:24-36`）

## 验收标准

1. 所有写 API 调用在 audit_log 表中有记录
2. `GET /api/v1/admin/audit?action=agent.create` 返回过滤结果
3. 租户超出月度 token 预算 → API 返回 `429 quota exceeded`
4. `GET /api/v1/admin/tenants/{id}/usage` 返回按模型分组的用量报告
5. 管理员暂停租户 → 该租户所有 API 返回 403
6. Vault 密钥更新后，密钥轮换器在 interval 内刷新

## 验证方法

```bash
# 审计查询
curl http://localhost:18789/api/v1/admin/audit?limit=10 \
  -H "Authorization: Bearer <admin-token>"

# 用量报告
curl http://localhost:18789/api/v1/admin/tenants/<id>/usage \
  -H "Authorization: Bearer <admin-token>"

# 租户暂停
curl -X PATCH http://localhost:18789/api/v1/admin/tenants/<id> \
  -H "Authorization: Bearer <admin-token>" \
  -d '{"status":"suspended"}'

# 验证暂停后请求被拒绝
curl http://localhost:18789/api/v1/agents \
  -H "Authorization: Bearer <suspended-tenant-token>"
# 预期 403
```

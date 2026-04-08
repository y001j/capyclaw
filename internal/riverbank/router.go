package riverbank

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"CapyClaw/internal/riverbank/middleware"
)

func (s *Server) setupRouter() (http.Handler, error) {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RealIP)
	r.Use(chimw.RequestID)
	r.Use(chimw.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.CORS(s.cfg.Riverbank.WebSocket.AllowedOrigins))

	// Health endpoints (unauthenticated)
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	// WebSocket endpoint
	r.Get("/ws", s.handleWebSocket)

	// Device pairing endpoints (unauthenticated)
	r.Post("/api/v1/device/code", s.handleDeviceCode)
	r.Post("/api/v1/device/token", s.handleDeviceToken)

	// Dev token endpoint (unauthenticated, only works in dev mode)
	r.Post("/api/v1/dev/token", s.handleDevToken)

	// Authenticated API routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(s.cfg.Riverbank.Auth))
		r.Use(middleware.Tenant())
		r.Use(middleware.RateLimit(s.cfg.Rapids, s.redis))
		r.Use(middleware.Audit(s.auditLogger))

		// Configuration
		r.Get("/api/v1/config/models", s.handleListModels)

		// OpenAI-compatible chat completions
		r.Post("/v1/chat/completions", s.handleChatCompletions)

		// Agent management
		r.Route("/api/v1/agents", func(r chi.Router) {
			r.Get("/", s.handleListAgents)
			r.Post("/", s.handleCreateAgent)
			r.Route("/{agentID}", func(r chi.Router) {
				r.Get("/", s.handleGetAgent)
				r.Patch("/", s.handleUpdateAgent)
				r.Delete("/", s.handleDeleteAgent)
				r.Get("/sessions", s.handleListAgentSessions)
				r.Post("/skills", s.handleAttachSkill)
				r.Delete("/skills/{skillID}", s.handleDetachSkill)
				r.Get("/memories", s.handleListMemories)
				r.Post("/memories", s.handleCreateMemory)
				r.Delete("/memories/{memoryID}", s.handleDeleteMemory)
				r.Post("/memories/search", s.handleSearchMemories)
				r.Get("/crons", s.handleListCronJobs)
				r.Post("/crons", s.handleCreateCronJob)
				r.Patch("/crons/{cronID}", s.handleUpdateCronJob)
				r.Delete("/crons/{cronID}", s.handleDeleteCronJob)
			})
		})

		// Cron jobs (global)
		r.Get("/api/v1/crons", s.handleListAllCronJobs)

		// Session management
		r.Route("/api/v1/sessions/{sessionID}", func(r chi.Router) {
			r.Get("/", s.handleGetSession)
			r.Get("/messages", s.handleListMessages)
			r.Delete("/", s.handleArchiveSession)
			r.Post("/compact", s.handleCompactSession)
		})

		// Skills
		r.Route("/api/v1/skills", func(r chi.Router) {
			r.Get("/", s.handleListSkills)
			r.Get("/{skillID}", s.handleGetSkill)
			r.Patch("/{skillID}", s.handleUpdateSkill)
			r.Post("/install", s.handleInstallSkill)
			r.Post("/upload", s.handleUploadSkill)
			r.Post("/parse-skillmd", s.handleParseSkillMD)
			r.Delete("/{skillID}", s.handleUninstallSkill)
		})

		// ClawHub marketplace proxy
		r.Route("/api/v1/clawhub", func(r chi.Router) {
			r.Get("/search", s.handleClawHubSearch)
			r.Get("/skills/{slug}", s.handleClawHubGetSkill)
			r.Get("/skills/{slug}/file", s.handleClawHubGetFile)
			r.Post("/install", s.handleClawHubInstall)
		})

		// Tools
		r.Post("/tools/invoke", s.handleInvokeTool)

		// Webhooks
		r.Post("/hooks/{path}", s.handleWebhook)

		// Administration
		r.Route("/api/v1/admin", func(r chi.Router) {
			r.Get("/tenants", s.handleListTenants)
			r.Post("/tenants", s.handleCreateTenant)
			r.Patch("/tenants/{tenantID}", s.handleUpdateTenant)
			r.Get("/tenants/{tenantID}/usage", s.handleTenantUsage)
			r.Get("/audit", s.handleQueryAudit)
			r.Get("/health/detailed", s.handleDetailedHealth)
			r.Post("/device/approve", s.handleDeviceApprove)
		})
	})

	return r, nil
}

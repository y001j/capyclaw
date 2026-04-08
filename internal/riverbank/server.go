package riverbank

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"CapyClaw/internal/burrow"
	"CapyClaw/internal/burrow/instinct"
	"CapyClaw/internal/burrow/llm"
	"CapyClaw/internal/burrow/llm/providers"
	"CapyClaw/internal/burrow/nibble"
	"CapyClaw/internal/lodge/drift"
	"CapyClaw/internal/pond"
	"CapyClaw/internal/pond/memory"
	"CapyClaw/internal/pond/pebble"
	"CapyClaw/internal/pond/ripple"
	"CapyClaw/internal/shared/config"
	"CapyClaw/internal/wetland/footprint"
	"CapyClaw/internal/wetland/marsh"
)

// Server is the main CapyClaw gateway server.
type Server struct {
	cfg    *config.Config
	router http.Handler
	http   *http.Server

	// Data layer
	db          *pond.DB
	agentRepo   *pebble.AgentRepository
	sessionRepo *pebble.SessionRepository
	messageRepo *pebble.MessageRepository

	// Platform services
	tenantMgr      *marsh.TenantManager
	quotaMgr       *marsh.QuotaManager
	billingTracker *marsh.BillingTracker
	auditLogger    *footprint.Logger

	// Memory
	memoryMgr *memory.Manager

	// Agent core
	compactor    *instinct.Compactor
	toolExecutor *nibble.Executor

	// Infrastructure
	redis       *redis.Client
	pipeline    *burrow.Pipeline
	cronMgr     *drift.CronManager
	driftWorker *drift.Worker
}

// NewServer creates a new gateway server with all routes and middleware configured.
func NewServer(ctx context.Context, cfg *config.Config) (*Server, error) {
	s := &Server{cfg: cfg}

	// Initialize database
	db, err := pond.New(ctx, cfg.Pond.Postgres)
	if err != nil {
		return nil, fmt.Errorf("initializing database: %w", err)
	}
	s.db = db

	// Initialize repositories
	s.agentRepo = pebble.NewAgentRepository(db.Pool)
	s.sessionRepo = pebble.NewSessionRepository(db.Pool)
	s.messageRepo = pebble.NewMessageRepository(db.Pool)

	// Initialize platform services
	s.tenantMgr = marsh.NewTenantManager(db.Pool)
	s.quotaMgr = marsh.NewQuotaManager(db.Pool)
	s.auditLogger = footprint.NewLogger(db.Pool)
	s.billingTracker = marsh.NewBillingTracker(db.Pool, s.quotaMgr, s.auditLogger)

	// Initialize Redis
	if len(cfg.Lodge.Redis.Addresses) > 0 {
		s.redis = redis.NewClient(&redis.Options{
			Addr:     cfg.Lodge.Redis.Addresses[0],
			Password: cfg.Lodge.Redis.Password,
			DB:       cfg.Lodge.Redis.DB,
			PoolSize: cfg.Lodge.Redis.PoolSize,
		})

		if err := s.redis.Ping(ctx).Err(); err != nil {
			slog.Warn("redis connection failed, rate limiting will be disabled", "error", err)
		} else {
			slog.Info("redis connection established", "addr", cfg.Lodge.Redis.Addresses[0])
		}
	}

	// Initialize LLM client
	llmClient := buildLLMClient(&cfg.Providers)

	// Initialize tool registry and register all built-in tools
	toolRegistry := nibble.NewRegistry()
	nibble.RegisterBuiltinTools(toolRegistry)
	nibble.RegisterSearchTools(&cfg.Search)

	toolTimeout, _ := time.ParseDuration(cfg.Burrow.ToolExecution.DefaultTimeout)
	if toolTimeout == 0 {
		toolTimeout = 30 * time.Second
	}
	s.toolExecutor = nibble.NewExecutor(toolRegistry, toolTimeout)

	// Initialize embedding and memory system
	var memoryExtractor *memory.Extractor
	if cfg.Pond.Vector.EmbeddingModel != "" && cfg.Pond.Vector.EmbeddingBaseURL != "" {
		embedder := ripple.NewOpenAIEmbedder(
			cfg.Pond.Vector.EmbeddingAPIKey,
			cfg.Pond.Vector.EmbeddingBaseURL,
			cfg.Pond.Vector.EmbeddingModel,
			cfg.Pond.Vector.EmbeddingDimensions,
		)
		searchEngine := ripple.NewSearchEngine(db.Pool, cfg.Pond.Vector.Search.MaxResults)
		episodicStore := memory.NewEpisodicStore(db.Pool, embedder.Generate)
		semanticStore := memory.NewSemanticStore(db.Pool, searchEngine, embedder.Generate)
		proceduralStore := memory.NewProceduralStore(db.Pool, embedder.Generate)
		s.memoryMgr = memory.NewManager(db.Pool, episodicStore, semanticStore, proceduralStore, embedder.Generate)

		// Create memory extractor (uses LLM to classify and extract memories from conversations)
		memoryExtractor = memory.NewExtractor(llmClient, cfg.Burrow.DefaultModel, s.memoryMgr, embedder.Generate)

		// Start periodic memory consolidation (every 6 hours)
		consolidator := memory.NewConsolidator(db.Pool, 500, 0.92, 0.05)
		go func() {
			ticker := time.NewTicker(6 * time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				if err := consolidator.ConsolidateAllAgents(context.Background()); err != nil {
					slog.Warn("memory consolidation failed", "error", err)
				}
			}
		}()

		slog.Info("memory system initialized",
			"embedding_model", cfg.Pond.Vector.EmbeddingModel,
			"dimensions", cfg.Pond.Vector.EmbeddingDimensions,
			"base_url", cfg.Pond.Vector.EmbeddingBaseURL,
		)
	}

	// Initialize compactor for session context compaction
	summaryModel := cfg.Burrow.Compaction.SummaryModel
	if summaryModel == "" {
		summaryModel = cfg.Burrow.DefaultModel
	}
	s.compactor = instinct.NewCompactor(summaryModel, llmClient)

	maxContextTokens := cfg.Burrow.MaxContextTokens
	if maxContextTokens <= 0 {
		maxContextTokens = 200000
	}
	triggerThreshold := cfg.Burrow.Compaction.TriggerThreshold
	if triggerThreshold <= 0 {
		triggerThreshold = 0.80
	}
	contextMgr := instinct.NewContextManager(maxContextTokens, triggerThreshold)

	// Initialize pipeline with all dependencies
	s.pipeline = burrow.NewPipeline(&burrow.PipelineDeps{
		SessionRepo:     s.sessionRepo,
		MessageRepo:     s.messageRepo,
		LLMClient:       llmClient,
		Config:          &cfg.Burrow,
		ToolRegistry:    toolRegistry,
		ToolExecutor:    s.toolExecutor,
		Pool:            s.db.Pool,
		MemoryMgr:       s.memoryMgr,
		MemoryExtractor: memoryExtractor,
		Compactor:       s.compactor,
		ContextMgr:      contextMgr,
	})

	// Wire up tool callbacks that need access to repositories

	// memory_search: let the agent search its own memories via tool call
	if s.memoryMgr != nil {
		mgr := s.memoryMgr
		nibble.SetMemorySearchFunc(func(ctx context.Context, agentIDStr, query string, limit int) ([]map[string]any, error) {
			agentID, err := uuid.Parse(agentIDStr)
			if err != nil {
				return nil, fmt.Errorf("invalid agent_id: %w", err)
			}
			entries, err := mgr.Recall(ctx, agentID, query, limit)
			if err != nil {
				return nil, err
			}
			results := make([]map[string]any, len(entries))
			for i, e := range entries {
				results[i] = map[string]any{
					"id":          e.ID.String(),
					"type":        e.MemoryType,
					"content":     e.Content,
					"importance":  e.ImportanceScore,
					"access_count": e.AccessCount,
				}
			}
			return results, nil
		})
	}

	// sessions_list / sessions_history / sessions_send
	sessionRepo := s.sessionRepo
	messageRepo := s.messageRepo
	nibble.SetSessionFuncs(
		// list
		func(ctx context.Context, agentIDStr string) ([]map[string]any, error) {
			agentID, err := uuid.Parse(agentIDStr)
			if err != nil {
				return nil, fmt.Errorf("invalid agent_id: %w", err)
			}
			sessions, err := sessionRepo.ListByAgent(ctx, agentID, 50, 0)
			if err != nil {
				return nil, err
			}
			results := make([]map[string]any, len(sessions))
			for i, sess := range sessions {
				results[i] = map[string]any{
					"id":            sess.ID.String(),
					"session_key":   sess.SessionKey,
					"status":        sess.Status,
					"input_tokens":  sess.TotalInputTokens,
					"output_tokens": sess.TotalOutputTokens,
					"created_at":    sess.CreatedAt.Format("2006-01-02 15:04:05"),
					"updated_at":    sess.UpdatedAt.Format("2006-01-02 15:04:05"),
				}
			}
			return results, nil
		},
		// history
		func(ctx context.Context, sessionIDStr string, limit int) ([]map[string]any, error) {
			sessionID, err := uuid.Parse(sessionIDStr)
			if err != nil {
				return nil, fmt.Errorf("invalid session_id: %w", err)
			}
			messages, err := messageRepo.ListBySession(ctx, sessionID, limit, 0)
			if err != nil {
				return nil, err
			}
			results := make([]map[string]any, len(messages))
			for i, msg := range messages {
				var content string
				if jsonErr := json.Unmarshal(msg.Content, &content); jsonErr != nil {
					content = string(msg.Content)
				}
				results[i] = map[string]any{
					"id":         msg.ID.String(),
					"role":       msg.Role,
					"content":    content,
					"created_at": msg.CreatedAt.Format("2006-01-02 15:04:05"),
				}
			}
			return results, nil
		},
		// send (delegates to pipeline)
		func(ctx context.Context, sessionIDStr, message string) (string, error) {
			// TODO: implement cross-session message send via pipeline
			return "", fmt.Errorf("cross-session send not yet implemented")
		},
	)

	// Wire up cron tools and start scheduler if Redis is available
	if s.redis != nil && len(cfg.Lodge.Redis.Addresses) > 0 {
		redisAddr := cfg.Lodge.Redis.Addresses[0]
		pool := db.Pool

		s.cronMgr = drift.NewCronManager(redisAddr, pool)

		nibble.SetCronFuncs(
			// create
			func(ctx context.Context, agentIDStr, name, schedule, action string) (string, error) {
				agentID, err := uuid.Parse(agentIDStr)
				if err != nil {
					return "", fmt.Errorf("invalid agent_id: %w", err)
				}
				// Insert into database
				var jobID, tenantID uuid.UUID
				err = pool.QueryRow(ctx, `
					INSERT INTO cron_jobs (agent_id, tenant_id, name, schedule, prompt, enabled)
					VALUES ($1, (SELECT tenant_id FROM agents WHERE id = $1), $2, $3, $4, true)
					RETURNING id, tenant_id
				`, agentID, name, schedule, action).Scan(&jobID, &tenantID)
				if err != nil {
					return "", fmt.Errorf("creating cron job: %w", err)
				}

				// Register with Asynq scheduler
				job := drift.CronJob{
					ID:       jobID,
					TenantID: tenantID,
					AgentID:  agentID,
					Name:     name,
					Schedule: schedule,
					Prompt:   action,
					Enabled:  true,
				}
				if err := s.cronMgr.ScheduleJob(job); err != nil {
					slog.Warn("failed to schedule cron job", "id", jobID, "error", err)
				}

				return fmt.Sprintf("Created cron job %s (id: %s, schedule: %s)", name, jobID, schedule), nil
			},
			// list
			func(ctx context.Context, agentIDStr string) ([]map[string]any, error) {
				agentID, err := uuid.Parse(agentIDStr)
				if err != nil {
					return nil, fmt.Errorf("invalid agent_id: %w", err)
				}
				rows, err := pool.Query(ctx, `
					SELECT id, name, schedule, prompt, enabled, last_run_at, run_count, last_status
					FROM cron_jobs WHERE agent_id = $1
					ORDER BY created_at DESC
				`, agentID)
				if err != nil {
					return nil, fmt.Errorf("listing cron jobs: %w", err)
				}
				defer rows.Close()

				var results []map[string]any
				for rows.Next() {
					var id uuid.UUID
					var name, schedule, prompt string
					var enabled bool
					var lastRunAt *time.Time
					var runCount int
					var lastStatus *string
					if err := rows.Scan(&id, &name, &schedule, &prompt, &enabled, &lastRunAt, &runCount, &lastStatus); err != nil {
						continue
					}
					entry := map[string]any{
						"id":        id.String(),
						"name":      name,
						"schedule":  schedule,
						"prompt":    prompt,
						"enabled":   enabled,
						"run_count": runCount,
					}
					if lastRunAt != nil {
						entry["last_run_at"] = lastRunAt.Format("2006-01-02 15:04:05")
					}
					if lastStatus != nil {
						entry["last_status"] = *lastStatus
					}
					results = append(results, entry)
				}
				return results, nil
			},
			// delete
			func(ctx context.Context, agentIDStr, cronIDStr string) error {
				cronID, err := uuid.Parse(cronIDStr)
				if err != nil {
					return fmt.Errorf("invalid cron_id: %w", err)
				}
				agentID, err := uuid.Parse(agentIDStr)
				if err != nil {
					return fmt.Errorf("invalid agent_id: %w", err)
				}
				tag, err := pool.Exec(ctx, `
					DELETE FROM cron_jobs WHERE id = $1 AND agent_id = $2
				`, cronID, agentID)
				if err != nil {
					return fmt.Errorf("deleting cron job: %w", err)
				}
				if tag.RowsAffected() == 0 {
					return fmt.Errorf("cron job not found")
				}
				// Unschedule from Asynq
				_ = s.cronMgr.UnscheduleJob(cronID)
				return nil
			},
		)

		// Load existing cron jobs and start scheduler
		if err := s.cronMgr.LoadAndSchedule(ctx); err != nil {
			slog.Warn("failed to load cron jobs", "error", err)
		}
		if err := s.cronMgr.Start(); err != nil {
			slog.Warn("failed to start cron scheduler", "error", err)
		}

		// Create and start Asynq worker to process cron tasks
		s.driftWorker = drift.NewWorker(redisAddr, nil, pool)
		s.driftWorker.SetPipeline(s.pipeline)
		if err := s.driftWorker.Start(); err != nil {
			slog.Warn("failed to start drift worker", "error", err)
		} else {
			slog.Info("drift worker started for cron task execution")
		}

		slog.Info("cron scheduler started", "redis", redisAddr)
	}

	// Setup router
	router, err := s.setupRouter()
	if err != nil {
		return nil, fmt.Errorf("setting up router: %w", err)
	}
	s.router = router

	return s, nil
}

// buildLLMClient creates the routed failover LLM client from provider config.
// Each provider's models list is used for direct model→provider routing.
func buildLLMClient(cfg *config.ProvidersConfig) llm.Client {
	var pwms []llm.ProviderWithModels

	if cfg.Anthropic.APIKey != "" {
		baseURL := cfg.Anthropic.BaseURL
		if baseURL == "" {
			baseURL = "https://api.anthropic.com"
		}
		pwms = append(pwms, llm.ProviderWithModels{
			Client: providers.NewAnthropicClient(cfg.Anthropic.APIKey, baseURL),
			Models: cfg.Anthropic.Models,
		})
	}
	if cfg.MiniMax.APIKey != "" {
		baseURL := cfg.MiniMax.BaseURL
		if baseURL == "" {
			baseURL = "https://api.minimaxi.com/anthropic"
		}
		pwms = append(pwms, llm.ProviderWithModels{
			Client: providers.NewAnthropicClient(cfg.MiniMax.APIKey, baseURL),
			Models: cfg.MiniMax.Models,
		})
	}
	if cfg.OpenAI.APIKey != "" {
		baseURL := cfg.OpenAI.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		pwms = append(pwms, llm.ProviderWithModels{
			Client: providers.NewOpenAIClient(cfg.OpenAI.APIKey, baseURL),
			Models: cfg.OpenAI.Models,
		})
	}
	if cfg.Google.APIKey != "" {
		pwms = append(pwms, llm.ProviderWithModels{
			Client: providers.NewGoogleClient(cfg.Google.APIKey, cfg.Google.BaseURL),
			Models: cfg.Google.Models,
		})
	}
	if cfg.Ollama.BaseURL != "" {
		pwms = append(pwms, llm.ProviderWithModels{
			Client: providers.NewOllamaClient(cfg.Ollama.BaseURL),
			Models: cfg.Ollama.Models,
		})
	}
	if cfg.VolcEngine.APIKey != "" {
		baseURL := cfg.VolcEngine.BaseURL
		if baseURL == "" {
			baseURL = "https://ark.cn-beijing.volces.com/api/v3"
		}
		pwms = append(pwms, llm.ProviderWithModels{
			Client: providers.NewVolcEngineClient(cfg.VolcEngine.APIKey, baseURL),
			Models: cfg.VolcEngine.Models,
		})
	}

	if len(pwms) == 0 {
		slog.Warn("no LLM providers configured, chat completions will not work")
		return nil
	}

	return llm.NewRoutedFailoverClient(pwms...)
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	s.http = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	if s.cfg.Riverbank.TLS.Enabled {
		slog.Info("TLS enabled", "min_version", s.cfg.Riverbank.TLS.MinVersion)
		return s.http.ListenAndServeTLS(
			s.cfg.Riverbank.TLS.CertPath,
			s.cfg.Riverbank.TLS.KeyPath,
		)
	}

	return s.http.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("shutting down gateway server")

	if s.http != nil {
		if err := s.http.Shutdown(ctx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
	}

	if s.cronMgr != nil {
		s.cronMgr.Shutdown()
	}
	if s.driftWorker != nil {
		s.driftWorker.Stop()
	}

	if s.redis != nil {
		if err := s.redis.Close(); err != nil {
			slog.Warn("redis close error", "error", err)
		}
	}

	if s.db != nil {
		s.db.Close()
	}

	slog.Info("gateway server shutdown complete")
	return nil
}

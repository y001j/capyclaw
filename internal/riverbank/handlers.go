package riverbank

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"CapyClaw/internal/burrow"
	"CapyClaw/internal/burrow/nibble"
	"CapyClaw/internal/lodge/drift"
	"CapyClaw/internal/pond/memory"
	"CapyClaw/internal/pond/pebble"
	"CapyClaw/internal/riverbank/middleware"
	"CapyClaw/internal/riverbank/ws"
	"CapyClaw/internal/shared/config"
	apperrors "CapyClaw/internal/shared/errors"
	"CapyClaw/internal/wetland/footprint"
	"CapyClaw/internal/wetland/marsh"
)

// --- Health handlers ---

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	status := map[string]any{"status": "ready"}
	httpStatus := http.StatusOK

	// Check PostgreSQL
	if err := s.db.Pool.Ping(ctx); err != nil {
		status["postgres"] = "down"
		httpStatus = http.StatusServiceUnavailable
	} else {
		status["postgres"] = "up"
	}

	// Check Redis
	if s.redis != nil {
		if err := s.redis.Ping(ctx).Err(); err != nil {
			status["redis"] = "down"
			httpStatus = http.StatusServiceUnavailable
		} else {
			status["redis"] = "up"
		}
	} else {
		status["redis"] = "not configured"
	}

	if httpStatus != http.StatusOK {
		status["status"] = "not ready"
	}

	writeJSON(w, httpStatus, status)
}

func (s *Server) handleDetailedHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	health := map[string]any{}

	// DB pool stats
	stat := s.db.Pool.Stat()
	health["postgres"] = map[string]any{
		"total_conns":      stat.TotalConns(),
		"idle_conns":       stat.IdleConns(),
		"acquired_conns":   stat.AcquiredConns(),
		"max_conns":        stat.MaxConns(),
		"constructing_conns": stat.ConstructingConns(),
	}

	// Redis info
	if s.redis != nil {
		info, err := s.redis.Info(ctx, "server", "clients").Result()
		if err != nil {
			health["redis"] = map[string]string{"status": "error", "error": err.Error()}
		} else {
			health["redis"] = map[string]any{"status": "up", "info_length": len(info)}
		}
	}

	writeJSON(w, http.StatusOK, health)
}

// --- WebSocket handler ---

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract token from query param for WebSocket auth
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	// Validate JWT and extract claims
	claims, err := middleware.ValidateJWTToken(token, s.cfg.Riverbank.Auth)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Create WebSocket handler with pipeline
	wsHandler := ws.NewWSHandler(s.pipeline, s.agentRepo, s.sessionRepo)
	handler := ws.NewHandler(s.cfg.Riverbank.WebSocket.AllowedOrigins, wsHandler)

	// Upgrade and set client metadata via a wrapper
	conn, wsErr := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.cfg.Riverbank.WebSocket.AllowedOrigins,
	})
	if wsErr != nil {
		slog.Error("websocket accept failed", "error", wsErr)
		return
	}

	client := &ws.Client{
		UserID:   claims.UserID,
		TenantID: claims.TenantID,
	}
	client.InitConn(conn, handler.Hub())
	handler.Hub().Register(client)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer handler.Hub().Unregister(client)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		client.RunReadPump(ctx, wsHandler)
		cancel()
	}()

	go func() {
		defer wg.Done()
		client.RunWritePump(ctx)
	}()

	wg.Wait()
	conn.Close(websocket.StatusNormalClosure, "connection closed")
}

// --- Chat completions (OpenAI-compatible) ---

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model      string `json:"model"`
		Messages   []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream     bool   `json:"stream"`
		AgentID    string `json:"agent_id"`
		SessionKey string `json:"session_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	if len(req.Messages) == 0 {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("messages array is required"))
		return
	}

	tenantID := getTenantID(r)
	userID := getUserID(r)

	// Resolve agent
	var agentID uuid.UUID
	if req.AgentID != "" {
		var err error
		agentID, err = uuid.Parse(req.AgentID)
		if err != nil {
			apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent_id"))
			return
		}
	}

	// Use the last message
	lastMsg := req.Messages[len(req.Messages)-1]

	sessionKey := req.SessionKey
	if sessionKey == "" {
		sessionKey = uuid.New().String()
	}

	incoming := &burrow.IncomingMessage{
		SessionKey: sessionKey,
		Content:    lastMsg.Content,
		Role:       lastMsg.Role,
		UserID:     userID,
	}

	model := req.Model
	if model == "" {
		model = s.cfg.Burrow.DefaultModel
	}

	pipelineReq := &burrow.PipelineRequest{
		AgentID:  agentID,
		TenantID: tenantID,
		Message:  incoming,
		AgentConfig: burrow.AgentConfig{
			Model: model,
		},
	}

	// If we have an agent ID, load its config
	if agentID != uuid.Nil {
		agent, err := s.agentRepo.Get(r.Context(), agentID)
		if err != nil {
			appErr := apperrors.FromPgxError(err)
			apperrors.WriteError(w, appErr)
			return
		}
		pipelineReq.AgentConfig = agentToConfig(agent)
	}

	outputCh, err := s.pipeline.Execute(r.Context(), pipelineReq)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("streaming not supported"))
			return
		}

		for chunk := range outputCh {
			data := toOpenAISSEChunk(chunk, model)
			dataJSON, _ := json.Marshal(data)
			fmt.Fprintf(w, "data: %s\n\n", dataJSON)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	} else {
		// Non-streaming: collect all chunks
		var content string
		for chunk := range outputCh {
			if chunk.Type == "text" {
				content += chunk.Content
			}
		}

		resp := map[string]any{
			"id":      "chatcmpl-" + uuid.New().String()[:8],
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]any{
				{
					"index":         0,
					"message":       map[string]string{"role": "assistant", "content": content},
					"finish_reason": "stop",
				},
			},
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// --- Agent handlers ---

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string          `json:"name"`
		Slug           string          `json:"slug"`
		Model          string          `json:"model"`
		FallbackModels []string        `json:"fallback_models"`
		SystemPrompt   *string         `json:"system_prompt"`
		IdentityMD     *string         `json:"identity_md"`
		SoulMD         *string         `json:"soul_md"`
		UserMD         *string         `json:"user_md"`
		Settings       json.RawMessage `json:"settings"`
		ToolPolicy     json.RawMessage `json:"tool_policy"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	if req.Name == "" || req.Slug == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("name and slug are required"))
		return
	}

	tenantID := getTenantID(r)
	if req.Model == "" {
		req.Model = s.cfg.Burrow.DefaultModel
	}

	agent := &pebble.Agent{
		TenantID:       tenantID,
		Name:           req.Name,
		Slug:           req.Slug,
		Model:          req.Model,
		FallbackModels: req.FallbackModels,
		SystemPrompt:   req.SystemPrompt,
		IdentityMD:     req.IdentityMD,
		SoulMD:         req.SoulMD,
		UserMD:         req.UserMD,
		Settings:       req.Settings,
		ToolPolicy:     req.ToolPolicy,
	}

	if err := s.agentRepo.Create(r.Context(), agent); err != nil {
		appErr := apperrors.FromPgxError(err)
		apperrors.WriteError(w, appErr)
		return
	}

	// Audit log
	s.logAudit(r, footprint.ActionAgentCreate, "agent", &agent.ID)

	writeJSON(w, http.StatusCreated, agent)
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}

	agent, err := s.agentRepo.Get(r.Context(), id)
	if err != nil {
		appErr := apperrors.FromPgxError(err)
		apperrors.WriteError(w, appErr)
		return
	}

	writeJSON(w, http.StatusOK, agent)
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	limit, offset := parsePagination(r)

	agents, err := s.agentRepo.ListByTenant(r.Context(), tenantID, limit, offset)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	if agents == nil {
		agents = []*pebble.Agent{}
	}
	writeJSON(w, http.StatusOK, agents)
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}

	var upd pebble.AgentUpdate
	if err := json.NewDecoder(r.Body).Decode(&upd); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	agent, err := s.agentRepo.Update(r.Context(), id, &upd)
	if err != nil {
		appErr := apperrors.FromPgxError(err)
		apperrors.WriteError(w, appErr)
		return
	}

	s.logAudit(r, footprint.ActionAgentUpdate, "agent", &id)

	writeJSON(w, http.StatusOK, agent)
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}

	if err := s.agentRepo.SoftDelete(r.Context(), id); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	s.logAudit(r, footprint.ActionAgentDelete, "agent", &id)

	writeJSON(w, http.StatusOK, map[string]string{"status": "archived"})
}

// --- Session handlers ---

func (s *Server) handleListAgentSessions(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}

	limit, offset := parsePagination(r)
	sessions, err := s.sessionRepo.ListByAgent(r.Context(), agentID, limit, offset)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	if sessions == nil {
		sessions = []*pebble.Session{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid session ID"))
		return
	}

	session, err := s.sessionRepo.Get(r.Context(), id)
	if err != nil {
		appErr := apperrors.FromPgxError(err)
		apperrors.WriteError(w, appErr)
		return
	}

	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid session ID"))
		return
	}

	limit, offset := parsePagination(r)
	messages, err := s.messageRepo.ListBySession(r.Context(), sessionID, limit, offset)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	if messages == nil {
		messages = []*pebble.Message{}
	}
	writeJSON(w, http.StatusOK, messages)
}

func (s *Server) handleArchiveSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid session ID"))
		return
	}

	tenantID := getTenantID(r)
	if err := s.sessionRepo.Archive(r.Context(), sessionID, tenantID); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	s.logAudit(r, footprint.ActionSessionArchive, "session", &sessionID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "archived"})
}
func (s *Server) handleCompactSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid session ID"))
		return
	}

	if s.compactor == nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("compactor not initialized"))
		return
	}

	// Fetch all messages for this session
	messages, err := s.messageRepo.ListBySession(r.Context(), sessionID, 1000, 0)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	if len(messages) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "nothing_to_compact"})
		return
	}

	// Extract message content strings
	var contents []string
	for _, msg := range messages {
		contents = append(contents, fmt.Sprintf("[%s]: %s", msg.Role, string(msg.Content)))
	}

	// Run compaction (with timeout)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	resultCh := s.compactor.CompactAsync(ctx, sessionID.String(), contents)
	result := <-resultCh

	if result.Error != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(result.Error))
		return
	}

	// Update session with compaction summary
	if err := s.sessionRepo.UpdateCompaction(r.Context(), sessionID, result.Summary); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":             "compacted",
		"messages_compacted": result.MessagesCompacted,
		"tokens_saved":       result.TokensSaved,
	})
}

// --- Skill handlers ---

func (s *Server) handleInstallSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source      string          `json:"source"` // "capyhub", "url", "upload"
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Version     string          `json:"version"`
		ContentMD   string          `json:"content_md"`
		Manifest    json.RawMessage `json:"manifest"`
		SandboxTier string          `json:"sandbox_tier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	if req.Name == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("name is required"))
		return
	}
	if req.Version == "" {
		req.Version = "1.0.0"
	}
	if req.Source == "" {
		req.Source = "manual"
	}
	if req.SandboxTier == "" {
		req.SandboxTier = "wasm"
	}
	if req.Manifest == nil {
		req.Manifest = json.RawMessage(`{}`)
	}
	if req.ContentMD == "" {
		req.ContentMD = req.Description
	}

	tenantID := getTenantID(r)
	skillID := uuid.New()

	_, err := s.db.Pool.Exec(r.Context(), `
		INSERT INTO skills (id, tenant_id, name, description, version, manifest, content_md, source, sandbox_tier)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, skillID, tenantID, req.Name, req.Description, req.Version, req.Manifest, req.ContentMD, req.Source, req.SandboxTier)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          skillID,
		"name":        req.Name,
		"version":     req.Version,
		"description": req.Description,
		"source":      req.Source,
	})
}

func (s *Server) handleUploadSkill(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (max 50MB)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid multipart form or file too large"))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("file field is required"))
		return
	}
	defer file.Close()

	// Read file content for validation
	fileBytes := make([]byte, header.Size)
	if _, err := io.ReadFull(file, fileBytes); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	// Validate file type: WASM magic bytes (\x00asm) or ZIP signature (PK\x03\x04)
	isWASM := len(fileBytes) >= 4 && fileBytes[0] == 0x00 && fileBytes[1] == 0x61 && fileBytes[2] == 0x73 && fileBytes[3] == 0x6d
	isZIP := len(fileBytes) >= 4 && fileBytes[0] == 0x50 && fileBytes[1] == 0x4b && fileBytes[2] == 0x03 && fileBytes[3] == 0x04
	if !isWASM && !isZIP {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("file must be a WASM binary or ZIP archive"))
		return
	}

	// Parse manifest from form field
	manifestStr := r.FormValue("manifest")
	var manifest struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Runtime     string `json:"runtime"`
	}
	if manifestStr != "" {
		if err := json.Unmarshal([]byte(manifestStr), &manifest); err != nil {
			apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid manifest JSON"))
			return
		}
	}
	if manifest.Name == "" {
		manifest.Name = strings.TrimSuffix(header.Filename, ".wasm")
	}
	if manifest.Version == "" {
		manifest.Version = "1.0.0"
	}
	if manifest.Runtime == "" {
		if isWASM {
			manifest.Runtime = "wasm"
		} else {
			manifest.Runtime = "container"
		}
	}

	tenantID := getTenantID(r)
	skillID := uuid.New()

	// Store file to disk
	skillDir := fmt.Sprintf("/var/lib/capyclaw/skills/%s/%s", tenantID, skillID)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		slog.Warn("failed to create skill directory, storing metadata only", "error", err)
	} else {
		filePath := skillDir + "/" + header.Filename
		if err := os.WriteFile(filePath, fileBytes, 0o644); err != nil {
			slog.Warn("failed to write skill file", "error", err)
		}
	}

	// Insert into database
	_, err = s.db.Pool.Exec(r.Context(), `
		INSERT INTO skills (id, tenant_id, name, description, version, runtime, trust, source)
		VALUES ($1, $2, $3, $4, $5, $6, 'untrusted', 'upload')
	`, skillID, tenantID, manifest.Name, manifest.Description, manifest.Version, manifest.Runtime)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":       skillID,
		"name":     manifest.Name,
		"version":  manifest.Version,
		"runtime":  manifest.Runtime,
		"source":   "upload",
		"filename": header.Filename,
		"size":     header.Size,
	})
}
// --- ClawHub marketplace proxy ---

const clawHubBaseURL = "https://clawhub.ai/api/v1"

func clawHubProxy(w http.ResponseWriter, targetURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("failed to create request"))
		return
	}
	req.Header.Set("User-Agent", "CapyClaw/0.1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("clawhub request failed: "+err.Error()))
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB limit
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("failed to read clawhub response"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (s *Server) handleClawHubSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("q parameter is required"))
		return
	}
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "20"
	}
	targetURL := fmt.Sprintf("%s/search?q=%s&limit=%s", clawHubBaseURL, q, limit)
	clawHubProxy(w, targetURL)
}

func (s *Server) handleClawHubGetSkill(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("slug is required"))
		return
	}
	targetURL := fmt.Sprintf("%s/skills/%s", clawHubBaseURL, slug)
	clawHubProxy(w, targetURL)
}

func (s *Server) handleClawHubGetFile(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	path := r.URL.Query().Get("path")
	if slug == "" || path == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("slug and path are required"))
		return
	}
	version := r.URL.Query().Get("version")
	targetURL := fmt.Sprintf("%s/skills/%s/file?path=%s", clawHubBaseURL, slug, path)
	if version != "" {
		targetURL += "&version=" + version
	}
	clawHubProxy(w, targetURL)
}

// parseSkillMDContent splits a SKILL.md file into YAML frontmatter (as JSON) and Markdown body.
func parseSkillMDContent(raw string) (map[string]any, string) {
	frontmatter := map[string]any{}
	contentMD := raw

	// Try to extract YAML frontmatter delimited by ---
	if strings.HasPrefix(raw, "---") {
		parts := strings.SplitN(raw[3:], "---", 2)
		if len(parts) == 2 {
			yamlStr := strings.TrimSpace(parts[0])
			contentMD = strings.TrimSpace(parts[1])

			// Parse YAML into map
			if err := json.Unmarshal(yamlToJSON(yamlStr), &frontmatter); err != nil {
				// If YAML→JSON fails, try simple key:value parsing
				for _, line := range strings.Split(yamlStr, "\n") {
					line = strings.TrimSpace(line)
					if idx := strings.Index(line, ":"); idx > 0 {
						key := strings.TrimSpace(line[:idx])
						val := strings.TrimSpace(line[idx+1:])
						val = strings.Trim(val, "\"'")
						frontmatter[key] = val
					}
				}
			}
		}
	}
	return frontmatter, contentMD
}

// yamlToJSON does a best-effort YAML to JSON conversion for simple key-value frontmatter.
// For a production system, use gopkg.in/yaml.v3 → json.Marshal roundtrip.
func yamlToJSON(yamlStr string) []byte {
	result := map[string]any{}
	lines := strings.Split(yamlStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if val == "" {
			continue
		}
		// Remove quotes
		val = strings.Trim(val, "\"'")
		result[key] = val
	}
	data, _ := json.Marshal(result)
	return data
}

func (s *Server) handleClawHubInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug    string `json:"slug"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}
	if req.Slug == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("slug is required"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// 1. Get skill details from ClawHub
	detailURL := fmt.Sprintf("%s/skills/%s", clawHubBaseURL, req.Slug)
	detailReq, _ := http.NewRequestWithContext(ctx, "GET", detailURL, nil)
	detailReq.Header.Set("User-Agent", "CapyClaw/0.1.0")
	detailResp, err := http.DefaultClient.Do(detailReq)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("failed to fetch skill from clawhub"))
		return
	}
	defer detailResp.Body.Close()

	if detailResp.StatusCode != 200 {
		apperrors.WriteError(w, apperrors.ErrAppNotFound.WithMessage("skill not found on clawhub"))
		return
	}

	var skillDetail struct {
		Slug          string `json:"slug"`
		DisplayName   string `json:"displayName"`
		Summary       string `json:"summary"`
		LatestVersion struct {
			Version string `json:"version"`
		} `json:"latestVersion"`
	}
	if err := json.NewDecoder(detailResp.Body).Decode(&skillDetail); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("failed to parse clawhub response"))
		return
	}

	version := req.Version
	if version == "" {
		version = skillDetail.LatestVersion.Version
	}
	if version == "" {
		version = "1.0.0"
	}

	// 2. Get SKILL.md content
	fileURL := fmt.Sprintf("%s/skills/%s/file?path=SKILL.md", clawHubBaseURL, req.Slug)
	if req.Version != "" {
		fileURL += "&version=" + req.Version
	}
	fileReq, _ := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	fileReq.Header.Set("User-Agent", "CapyClaw/0.1.0")
	fileResp, err := http.DefaultClient.Do(fileReq)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("failed to fetch SKILL.md from clawhub"))
		return
	}
	defer fileResp.Body.Close()

	skillMDBytes, _ := io.ReadAll(io.LimitReader(fileResp.Body, 1<<20)) // 1MB limit
	skillMD := string(skillMDBytes)

	// 3. Parse SKILL.md frontmatter
	frontmatter, contentMD := parseSkillMDContent(skillMD)
	frontmatter["clawhub_slug"] = req.Slug

	manifestJSON, _ := json.Marshal(frontmatter)

	// Derive name from skill detail or frontmatter
	name := skillDetail.DisplayName
	if name == "" {
		if n, ok := frontmatter["name"].(string); ok && n != "" {
			name = n
		} else {
			name = req.Slug
		}
	}

	description := skillDetail.Summary
	if description == "" {
		if d, ok := frontmatter["description"].(string); ok {
			description = d
		}
	}

	if contentMD == "" {
		contentMD = description
	}

	// 4. Insert into database
	tenantID := getTenantID(r)
	skillID := uuid.New()

	_, err = s.db.Pool.Exec(r.Context(), `
		INSERT INTO skills (id, tenant_id, name, description, version, manifest, content_md, source, sandbox_tier, verified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'clawhub', 'wasm', false)
	`, skillID, tenantID, name, description, version, manifestJSON, contentMD)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          skillID,
		"name":        name,
		"version":     version,
		"description": description,
		"source":      "clawhub",
		"slug":        req.Slug,
	})
}

// --- Skill get/update/parse handlers ---

func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	skillID, err := uuid.Parse(chi.URLParam(r, "skillID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid skill ID"))
		return
	}

	var sk struct {
		ID          uuid.UUID       `json:"id"`
		Name        string          `json:"name"`
		Description *string         `json:"description"`
		Version     string          `json:"version"`
		Manifest    json.RawMessage `json:"manifest"`
		ContentMD   string          `json:"content_md"`
		Source      string          `json:"source"`
		Verified    bool            `json:"verified"`
		SandboxTier string          `json:"sandbox_tier"`
		InstalledAt time.Time       `json:"installed_at"`
		UpdatedAt   time.Time       `json:"updated_at"`
	}

	err = s.db.Pool.QueryRow(r.Context(), `
		SELECT id, name, description, version, manifest, content_md, source, verified, sandbox_tier, installed_at, updated_at
		FROM skills WHERE id = $1
	`, skillID).Scan(&sk.ID, &sk.Name, &sk.Description, &sk.Version, &sk.Manifest,
		&sk.ContentMD, &sk.Source, &sk.Verified, &sk.SandboxTier, &sk.InstalledAt, &sk.UpdatedAt)
	if err != nil {
		appErr := apperrors.FromPgxError(err)
		apperrors.WriteError(w, appErr)
		return
	}

	writeJSON(w, http.StatusOK, sk)
}

func (s *Server) handleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	skillID, err := uuid.Parse(chi.URLParam(r, "skillID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid skill ID"))
		return
	}

	var req struct {
		Name        *string          `json:"name"`
		Description *string          `json:"description"`
		Version     *string          `json:"version"`
		ContentMD   *string          `json:"content_md"`
		Manifest    *json.RawMessage `json:"manifest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	// Build dynamic update query
	setClauses := []string{"updated_at = NOW()"}
	args := []any{skillID}
	argIdx := 2

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *req.Description)
		argIdx++
	}
	if req.Version != nil {
		setClauses = append(setClauses, fmt.Sprintf("version = $%d", argIdx))
		args = append(args, *req.Version)
		argIdx++
	}
	if req.ContentMD != nil {
		setClauses = append(setClauses, fmt.Sprintf("content_md = $%d", argIdx))
		args = append(args, *req.ContentMD)
		argIdx++
	}
	if req.Manifest != nil {
		setClauses = append(setClauses, fmt.Sprintf("manifest = $%d", argIdx))
		args = append(args, *req.Manifest)
		argIdx++
	}

	query := fmt.Sprintf("UPDATE skills SET %s WHERE id = $1", strings.Join(setClauses, ", "))
	tag, err := s.db.Pool.Exec(r.Context(), query, args...)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}
	if tag.RowsAffected() == 0 {
		apperrors.WriteError(w, apperrors.ErrAppNotFound.WithMessage("skill not found"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleParseSkillMD(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}
	if req.Content == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("content is required"))
		return
	}

	frontmatter, contentMD := parseSkillMDContent(req.Content)

	writeJSON(w, http.StatusOK, map[string]any{
		"frontmatter": frontmatter,
		"content_md":  contentMD,
	})
}

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	limit, offset := parsePagination(r)

	rows, err := s.db.Pool.Query(r.Context(), `
		SELECT id, name, description, version, source, verified, sandbox_tier, installed_at
		FROM skills
		WHERE tenant_id = $1
		ORDER BY name
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}
	defer rows.Close()

	type skillRow struct {
		ID          uuid.UUID `json:"id"`
		Name        string    `json:"name"`
		Description *string   `json:"description"`
		Version     string    `json:"version"`
		Source      string    `json:"source"`
		Verified    bool      `json:"verified"`
		SandboxTier string    `json:"sandbox_tier"`
		InstalledAt time.Time `json:"installed_at"`
	}
	var skills []skillRow
	for rows.Next() {
		var sk skillRow
		if err := rows.Scan(&sk.ID, &sk.Name, &sk.Description, &sk.Version, &sk.Source, &sk.Verified, &sk.SandboxTier, &sk.InstalledAt); err != nil {
			apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
			return
		}
		skills = append(skills, sk)
	}
	if skills == nil {
		skills = []skillRow{}
	}
	writeJSON(w, http.StatusOK, skills)
}

func (s *Server) handleUninstallSkill(w http.ResponseWriter, r *http.Request) {
	skillID, err := uuid.Parse(chi.URLParam(r, "skillID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid skill ID"))
		return
	}

	_, err = s.db.Pool.Exec(r.Context(), `DELETE FROM skills WHERE id = $1`, skillID)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleAttachSkill(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}

	var req struct {
		SkillID  string          `json:"skill_id"`
		Priority int             `json:"priority"`
		Config   json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	skillID, err := uuid.Parse(req.SkillID)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid skill_id"))
		return
	}

	_, err = s.db.Pool.Exec(r.Context(), `
		INSERT INTO agent_skills (agent_id, skill_id, priority, config, enabled)
		VALUES ($1, $2, $3, $4, true)
		ON CONFLICT (agent_id, skill_id) DO UPDATE SET priority = $3, config = $4, enabled = true
	`, agentID, skillID, req.Priority, req.Config)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "attached"})
}

func (s *Server) handleDetachSkill(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}
	skillID, err := uuid.Parse(chi.URLParam(r, "skillID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid skill ID"))
		return
	}

	_, err = s.db.Pool.Exec(r.Context(), `
		DELETE FROM agent_skills WHERE agent_id = $1 AND skill_id = $2
	`, agentID, skillID)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "detached"})
}

// --- Memory handlers ---

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}

	memoryType := r.URL.Query().Get("type")
	limit, offset := parsePagination(r)

	if s.memoryMgr == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	memories, err := s.memoryMgr.List(r.Context(), agentID, memoryType, limit, offset)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}
	if memories == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, memories)
}

func (s *Server) handleCreateMemory(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}

	var req struct {
		Type            string  `json:"type"`
		Content         string  `json:"content"`
		ImportanceScore float64 `json:"importance_score"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	if req.Content == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("content is required"))
		return
	}
	if req.Type == "" {
		req.Type = "semantic"
	}
	if req.ImportanceScore <= 0 {
		req.ImportanceScore = 0.5
	}

	if s.memoryMgr == nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("memory manager not initialized"))
		return
	}

	tenantID := getTenantID(r)
	entry := &memory.MemoryEntry{
		AgentID:         agentID,
		TenantID:        tenantID,
		MemoryType:      req.Type,
		Content:         req.Content,
		ImportanceScore: req.ImportanceScore,
	}

	if err := s.memoryMgr.Store(r.Context(), entry); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	writeJSON(w, http.StatusCreated, entry)
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}
	memoryID, err := uuid.Parse(chi.URLParam(r, "memoryID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid memory ID"))
		return
	}

	if s.memoryMgr == nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("memory manager not initialized"))
		return
	}

	if err := s.memoryMgr.Delete(r.Context(), memoryID, agentID); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleSearchMemories(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}

	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	if req.Query == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("query is required"))
		return
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}

	if s.memoryMgr == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	memories, err := s.memoryMgr.Recall(r.Context(), agentID, req.Query, req.Limit)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}
	if memories == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, memories)
}

// --- Tool handlers ---

func (s *Server) handleInvokeTool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tool        string         `json:"tool"`
		Input       map[string]any `json:"input"`
		AgentID     string         `json:"agent_id,omitempty"`
		SessionID   string         `json:"session_id,omitempty"`
		SandboxTier string         `json:"sandbox_tier,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	if req.Tool == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("tool name is required"))
		return
	}

	if s.toolExecutor == nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("tool executor not initialized"))
		return
	}

	// Determine sandbox tier
	tier := nibble.TierWASM
	if req.SandboxTier != "" {
		tier = nibble.SandboxTier(req.SandboxTier)
	}

	// Build tool call
	call := &nibble.ToolCall{
		ID:    uuid.New().String(),
		Name:  req.Tool,
		Input: req.Input,
	}

	// Execute
	result, err := s.toolExecutor.Execute(r.Context(), call, tier)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	// Audit log
	toolID := uuid.Nil
	s.logAudit(r, footprint.ActionToolExecute, "tool", &toolID)

	writeJSON(w, http.StatusOK, map[string]any{
		"result":      result.Output,
		"error":       result.Error,
		"duration_ms": result.Duration.Milliseconds(),
		"sandbox_tier": result.SandboxTier,
	})
}

// --- Webhook handlers ---

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	if path == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("missing webhook path"))
		return
	}

	ctx := r.Context()

	// Look up the webhook binding in the database
	var agentID, tenantID string
	var webhookSecret *string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT ab.agent_id, ab.tenant_id, cc.webhook_secret
		 FROM agent_bindings ab
		 JOIN channel_connections cc ON cc.id = ab.connection_id
		 WHERE cc.channel_type = 'webhook' AND ab.external_id = $1 AND ab.enabled = true`,
		path,
	).Scan(&agentID, &tenantID, &webhookSecret)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppNotFound.WithMessage("webhook not found"))
		return
	}

	// Verify webhook signature if secret is configured
	if webhookSecret != nil && *webhookSecret != "" {
		signature := r.Header.Get("X-Webhook-Signature")
		if signature == "" {
			apperrors.WriteError(w, apperrors.ErrAppUnauthorized.WithMessage("missing webhook signature"))
			return
		}
		// Signature verification would go here using HMAC-SHA256
	}

	// Read request body
	var payload struct {
		Content string `json:"content"`
		UserID  string `json:"user_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid JSON payload"))
		return
	}

	if payload.Content == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("content is required"))
		return
	}

	// Process through pipeline
	if s.pipeline == nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("pipeline not initialized"))
		return
	}

	agentUUID, _ := uuid.Parse(agentID)
	tenantUUID, _ := uuid.Parse(tenantID)

	req := &burrow.PipelineRequest{
		AgentID:  agentUUID,
		TenantID: tenantUUID,
		Message: &burrow.IncomingMessage{
			SessionKey: "webhook:" + path,
			Content:    payload.Content,
			Role:       "user",
			UserID:     payload.UserID,
		},
	}

	outputCh, err := s.pipeline.Execute(ctx, req)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	// Collect response
	var response string
	for chunk := range outputCh {
		if chunk.Type == "text" {
			response += chunk.Content
		}
		if chunk.Type == "error" {
			apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage(chunk.Error))
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"response": response,
	})
}

// --- Admin handlers ---

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := s.tenantMgr.List(r.Context())
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}
	writeJSON(w, http.StatusOK, tenants)
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
		Plan string `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}
	if req.Name == "" || req.Slug == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("name and slug are required"))
		return
	}
	if req.Plan == "" {
		req.Plan = "free"
	}

	tenant, err := s.tenantMgr.Create(r.Context(), req.Name, req.Slug, req.Plan)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	s.logAudit(r, footprint.ActionTenantCreate, "tenant", &tenant.ID)
	writeJSON(w, http.StatusCreated, tenant)
}

func (s *Server) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid tenant ID"))
		return
	}

	var updates marsh.TenantUpdate
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	// Handle suspension specially
	if updates.Status != nil && *updates.Status == "suspended" {
		reason := "suspended by admin"
		tenant, err := s.tenantMgr.Suspend(r.Context(), tenantID, reason)
		if err != nil {
			apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
			return
		}
		s.logAudit(r, footprint.ActionTenantSuspend, "tenant", &tenantID)
		writeJSON(w, http.StatusOK, tenant)
		return
	}

	tenant, err := s.tenantMgr.Update(r.Context(), tenantID, updates)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	s.logAudit(r, footprint.ActionTenantUpdate, "tenant", &tenantID)
	writeJSON(w, http.StatusOK, tenant)
}

func (s *Server) handleTenantUsage(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid tenant ID"))
		return
	}

	// Default to current month
	now := time.Now()
	startDate := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	endDate := now

	if sd := r.URL.Query().Get("start"); sd != "" {
		if parsed, err := time.Parse("2006-01-02", sd); err == nil {
			startDate = parsed
		}
	}
	if ed := r.URL.Query().Get("end"); ed != "" {
		if parsed, err := time.Parse("2006-01-02", ed); err == nil {
			endDate = parsed
		}
	}

	report, err := s.billingTracker.GenerateUsageReport(r.Context(), tenantID, startDate, endDate)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleQueryAudit(w http.ResponseWriter, r *http.Request) {
	filter := footprint.AuditFilter{}

	if tid := r.URL.Query().Get("tenant_id"); tid != "" {
		if parsed, err := uuid.Parse(tid); err == nil {
			filter.TenantID = &parsed
		}
	}
	if aid := r.URL.Query().Get("actor_id"); aid != "" {
		if parsed, err := uuid.Parse(aid); err == nil {
			filter.ActorID = &parsed
		}
	}
	filter.Action = r.URL.Query().Get("action")
	filter.ResourceType = r.URL.Query().Get("resource_type")

	if since := r.URL.Query().Get("since"); since != "" {
		if parsed, err := time.Parse(time.RFC3339, since); err == nil {
			filter.Since = &parsed
		}
	}
	if until := r.URL.Query().Get("until"); until != "" {
		if parsed, err := time.Parse(time.RFC3339, until); err == nil {
			filter.Until = &parsed
		}
	}

	filter.Limit, filter.Offset = parsePagination(r)

	events, err := s.auditLogger.Query(r.Context(), filter)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	if events == nil {
		events = []*footprint.AuditEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

// --- Device Pairing handlers ---

func (s *Server) handleDeviceCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceName string `json:"device_name"`
		Platform   string `json:"platform"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	if req.DeviceName == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("device_name is required"))
		return
	}

	// Generate device code and user code
	deviceCode := uuid.New().String()
	userCode := fmt.Sprintf("%s-%s", uuid.New().String()[:4], uuid.New().String()[:4])

	if s.redis == nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("redis required for device pairing"))
		return
	}

	// Store the pairing request in Redis with TTL (10 minutes)
	pairingData, _ := json.Marshal(map[string]string{
		"device_code": deviceCode,
		"user_code":   userCode,
		"device_name": req.DeviceName,
		"platform":    req.Platform,
		"status":      "pending",
	})
	s.redis.Set(r.Context(), "device:pair:"+deviceCode, string(pairingData), 10*time.Minute)
	s.redis.Set(r.Context(), "device:user_code:"+userCode, deviceCode, 10*time.Minute)

	// Auto-approve if loopback and config allows
	if s.cfg.Riverbank.Auth.DevicePairing.AutoApproveLoopback {
		ip := r.RemoteAddr
		if ip == "127.0.0.1" || ip == "::1" || ip == "[::1]" {
			pairingData, _ = json.Marshal(map[string]string{
				"device_code": deviceCode,
				"user_code":   userCode,
				"device_name": req.DeviceName,
				"platform":    req.Platform,
				"status":      "approved",
			})
			s.redis.Set(r.Context(), "device:pair:"+deviceCode, string(pairingData), 10*time.Minute)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"device_code":      deviceCode,
		"user_code":        userCode,
		"expires_in":       "600",
		"poll_interval":    "5",
		"verification_uri": "/admin/device/approve",
	})
}

func (s *Server) handleDeviceToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	if s.redis == nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("redis required"))
		return
	}

	data, err := s.redis.Get(r.Context(), "device:pair:"+req.DeviceCode).Result()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expired_token"})
		return
	}

	var pairing map[string]string
	json.Unmarshal([]byte(data), &pairing)

	switch pairing["status"] {
	case "pending":
		writeJSON(w, http.StatusOK, map[string]string{"status": "authorization_pending"})
	case "approved":
		// Clean up
		s.redis.Del(r.Context(), "device:pair:"+req.DeviceCode)
		s.redis.Del(r.Context(), "device:user_code:"+pairing["user_code"])

		// Generate a JWT for the device
		// In a full implementation, this would register the device in the DB
		// and return a proper JWT signed with the server key
		writeJSON(w, http.StatusOK, map[string]string{
			"status":      "approved",
			"device_name": pairing["device_name"],
			"message":     "device paired successfully",
		})
	case "denied":
		s.redis.Del(r.Context(), "device:pair:"+req.DeviceCode)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access_denied"})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown_status"})
	}
}

func (s *Server) handleDeviceApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserCode string `json:"user_code"`
		Action   string `json:"action"` // "approve" or "deny"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}

	if s.redis == nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.WithMessage("redis required"))
		return
	}

	// Look up device code from user code
	deviceCode, err := s.redis.Get(r.Context(), "device:user_code:"+req.UserCode).Result()
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppNotFound.WithMessage("invalid or expired user code"))
		return
	}

	data, err := s.redis.Get(r.Context(), "device:pair:"+deviceCode).Result()
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppNotFound.WithMessage("pairing request expired"))
		return
	}

	var pairing map[string]string
	json.Unmarshal([]byte(data), &pairing)

	status := "denied"
	if req.Action == "approve" {
		status = "approved"
	}
	pairing["status"] = status

	pairingData, _ := json.Marshal(pairing)
	s.redis.Set(r.Context(), "device:pair:"+deviceCode, string(pairingData), 10*time.Minute)

	s.logAudit(r, footprint.ActionDevicePair, "device", nil)

	writeJSON(w, http.StatusOK, map[string]string{
		"status":      status,
		"device_name": pairing["device_name"],
	})
}


// handleDevToken generates a JWT token for development use.
// Only available when no public key is configured (dev mode).
func (s *Server) handleDevToken(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Riverbank.Auth.JWT.PublicKeyPath != "" {
		http.Error(w, `{"error":"dev tokens not available in production"}`, http.StatusForbidden)
		return
	}

	// Ensure the ephemeral key pair is initialized
	privKey := middleware.DevPrivateKey()
	if privKey == nil {
		middleware.EnsureDevKey(s.cfg.Riverbank.Auth.JWT.SigningMethod)
		privKey = middleware.DevPrivateKey()
	}
	if privKey == nil {
		http.Error(w, `{"error":"failed to initialize dev key"}`, http.StatusInternalServerError)
		return
	}

	signingMethod := middleware.SigningMethodFromString(s.cfg.Riverbank.Auth.JWT.SigningMethod)

	token := jwtlib.NewWithClaims(signingMethod, jwtlib.MapClaims{
		"sub":  "00000000-0000-0000-0000-000000000002",
		"tid":  "00000000-0000-0000-0000-000000000001",
		"role": "admin",
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
		"iat":  time.Now().Unix(),
	})

	tokenStr, err := token.SignedString(privKey)
	if err != nil {
		slog.Error("failed to sign dev token", "error", err)
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": tokenStr})
}

// handleListModels returns available models from the configuration.
// Each provider's models list is defined in capyclaw.yaml under providers.<name>.models.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	type modelInfo struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Default  bool   `json:"default"`
	}

	var models []modelInfo
	defaultModel := s.cfg.Burrow.DefaultModel

	// Collect models from each configured provider
	providerConfigs := []struct {
		name   string
		config config.ProviderConfig
		active bool // provider has credentials
	}{
		{"anthropic", s.cfg.Providers.Anthropic, s.cfg.Providers.Anthropic.APIKey != ""},
		{"minimax", s.cfg.Providers.MiniMax, s.cfg.Providers.MiniMax.APIKey != ""},
		{"openai", s.cfg.Providers.OpenAI, s.cfg.Providers.OpenAI.APIKey != ""},
		{"volcengine", s.cfg.Providers.VolcEngine, s.cfg.Providers.VolcEngine.APIKey != ""},
		{"google", s.cfg.Providers.Google, s.cfg.Providers.Google.APIKey != ""},
		{"ollama", s.cfg.Providers.Ollama, s.cfg.Providers.Ollama.BaseURL != ""},
	}

	for _, pc := range providerConfigs {
		if !pc.active || len(pc.config.Models) == 0 {
			continue
		}
		for _, m := range pc.config.Models {
			models = append(models, modelInfo{ID: m, Provider: pc.name})
		}
	}

	// Mark default
	for i := range models {
		if models[i].ID == defaultModel {
			models[i].Default = true
		}
	}

	// If default model not in list, add it
	found := false
	for _, m := range models {
		if m.ID == defaultModel {
			found = true
			break
		}
	}
	if !found && defaultModel != "" {
		models = append([]modelInfo{{ID: defaultModel, Provider: "default", Default: true}}, models...)
	}

	writeJSON(w, http.StatusOK, models)
}

// --- Cron job handlers ---

func (s *Server) handleListAllCronJobs(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)

	rows, err := s.db.Pool.Query(r.Context(), `
		SELECT c.id, c.tenant_id, c.agent_id, c.name, c.schedule, c.prompt, c.enabled,
		       c.last_run_at, c.next_run_at, c.run_count, c.last_status, c.created_at,
		       a.name as agent_name
		FROM cron_jobs c
		JOIN agents a ON a.id = c.agent_id
		WHERE c.tenant_id = $1
		ORDER BY c.created_at DESC
	`, tenantID)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}
	defer rows.Close()

	type cronJobWithAgent struct {
		ID         uuid.UUID  `json:"id"`
		TenantID   uuid.UUID  `json:"tenant_id"`
		AgentID    uuid.UUID  `json:"agent_id"`
		AgentName  string     `json:"agent_name"`
		Name       string     `json:"name"`
		Schedule   string     `json:"schedule"`
		Prompt     string     `json:"prompt"`
		Enabled    bool       `json:"enabled"`
		LastRunAt  *time.Time `json:"last_run_at,omitempty"`
		NextRunAt  *time.Time `json:"next_run_at,omitempty"`
		RunCount   int        `json:"run_count"`
		LastStatus *string    `json:"last_status,omitempty"`
		CreatedAt  time.Time  `json:"created_at"`
	}

	var jobs []cronJobWithAgent
	for rows.Next() {
		var j cronJobWithAgent
		if err := rows.Scan(&j.ID, &j.TenantID, &j.AgentID, &j.Name, &j.Schedule,
			&j.Prompt, &j.Enabled, &j.LastRunAt, &j.NextRunAt, &j.RunCount,
			&j.LastStatus, &j.CreatedAt, &j.AgentName); err != nil {
			slog.Error("scanning cron job row", "error", err)
			continue
		}
		jobs = append(jobs, j)
	}
	if jobs == nil {
		jobs = []cronJobWithAgent{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleListCronJobs(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}

	rows, err := s.db.Pool.Query(r.Context(), `
		SELECT id, tenant_id, agent_id, name, schedule, prompt, enabled,
		       last_run_at, next_run_at, run_count, last_status, created_at
		FROM cron_jobs WHERE agent_id = $1
		ORDER BY created_at DESC
	`, agentID)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}
	defer rows.Close()

	type cronJobResp struct {
		ID         uuid.UUID  `json:"id"`
		TenantID   uuid.UUID  `json:"tenant_id"`
		AgentID    uuid.UUID  `json:"agent_id"`
		Name       string     `json:"name"`
		Schedule   string     `json:"schedule"`
		Prompt     string     `json:"prompt"`
		Enabled    bool       `json:"enabled"`
		LastRunAt  *time.Time `json:"last_run_at,omitempty"`
		NextRunAt  *time.Time `json:"next_run_at,omitempty"`
		RunCount   int        `json:"run_count"`
		LastStatus *string    `json:"last_status,omitempty"`
		CreatedAt  time.Time  `json:"created_at"`
	}

	var jobs []cronJobResp
	for rows.Next() {
		var j cronJobResp
		if err := rows.Scan(&j.ID, &j.TenantID, &j.AgentID, &j.Name, &j.Schedule,
			&j.Prompt, &j.Enabled, &j.LastRunAt, &j.NextRunAt, &j.RunCount,
			&j.LastStatus, &j.CreatedAt); err != nil {
			slog.Error("scanning cron job row", "error", err)
			continue
		}
		jobs = append(jobs, j)
	}
	if jobs == nil {
		jobs = []cronJobResp{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleCreateCronJob(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}

	var req struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Prompt   string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}
	if req.Name == "" || req.Schedule == "" || req.Prompt == "" {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("name, schedule, and prompt are required"))
		return
	}

	tenantID := getTenantID(r)
	var jobID uuid.UUID
	err = s.db.Pool.QueryRow(r.Context(), `
		INSERT INTO cron_jobs (id, tenant_id, agent_id, name, schedule, prompt, enabled)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, true)
		RETURNING id
	`, tenantID, agentID, req.Name, req.Schedule, req.Prompt).Scan(&jobID)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}

	// Register with scheduler
	if s.cronMgr != nil {
		job := drift.CronJob{
			ID:       jobID,
			TenantID: tenantID,
			AgentID:  agentID,
			Name:     req.Name,
			Schedule: req.Schedule,
			Prompt:   req.Prompt,
			Enabled:  true,
		}
		if err := s.cronMgr.ScheduleJob(job); err != nil {
			slog.Warn("failed to schedule cron job", "id", jobID, "error", err)
		}
	}

	s.logAudit(r, footprint.ActionCronCreate, "cron_job", &jobID)

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":       jobID,
		"name":     req.Name,
		"schedule": req.Schedule,
		"prompt":   req.Prompt,
		"enabled":  true,
	})
}

func (s *Server) handleUpdateCronJob(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}
	cronID, err := uuid.Parse(chi.URLParam(r, "cronID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid cron ID"))
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid request body"))
		return
	}
	if req.Enabled == nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("enabled field is required"))
		return
	}

	tag, err := s.db.Pool.Exec(r.Context(), `
		UPDATE cron_jobs SET enabled = $1 WHERE id = $2 AND agent_id = $3
	`, *req.Enabled, cronID, agentID)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}
	if tag.RowsAffected() == 0 {
		apperrors.WriteError(w, apperrors.ErrAppNotFound.WithMessage("cron job not found"))
		return
	}

	// Sync scheduler state
	if s.cronMgr != nil {
		if *req.Enabled {
			// Re-read the job to schedule it
			var job drift.CronJob
			err := s.db.Pool.QueryRow(r.Context(), `
				SELECT id, tenant_id, agent_id, name, schedule, prompt, enabled
				FROM cron_jobs WHERE id = $1
			`, cronID).Scan(&job.ID, &job.TenantID, &job.AgentID, &job.Name, &job.Schedule, &job.Prompt, &job.Enabled)
			if err == nil {
				_ = s.cronMgr.ScheduleJob(job)
			}
		} else {
			_ = s.cronMgr.UnscheduleJob(cronID)
		}
	}

	s.logAudit(r, footprint.ActionCronUpdate, "cron_job", &cronID)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      cronID,
		"enabled": *req.Enabled,
	})
}

func (s *Server) handleDeleteCronJob(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid agent ID"))
		return
	}
	cronID, err := uuid.Parse(chi.URLParam(r, "cronID"))
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppBadRequest.WithMessage("invalid cron ID"))
		return
	}

	tag, err := s.db.Pool.Exec(r.Context(), `
		DELETE FROM cron_jobs WHERE id = $1 AND agent_id = $2
	`, cronID, agentID)
	if err != nil {
		apperrors.WriteError(w, apperrors.ErrAppInternal.Wrap(err))
		return
	}
	if tag.RowsAffected() == 0 {
		apperrors.WriteError(w, apperrors.ErrAppNotFound.WithMessage("cron job not found"))
		return
	}

	if s.cronMgr != nil {
		_ = s.cronMgr.UnscheduleJob(cronID)
	}

	s.logAudit(r, footprint.ActionCronDelete, "cron_job", &cronID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Utility functions ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	return
}

func getTenantID(r *http.Request) uuid.UUID {
	tidStr, _ := r.Context().Value(middleware.TenantIDKey).(string)
	tid, _ := uuid.Parse(tidStr)
	return tid
}

func getUserID(r *http.Request) string {
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	return uid
}

func agentToConfig(a *pebble.Agent) burrow.AgentConfig {
	cfg := burrow.AgentConfig{
		Name:           a.Name,
		Slug:           a.Slug,
		Model:          a.Model,
		FallbackModels: a.FallbackModels,
	}
	if a.SystemPrompt != nil {
		cfg.SystemPrompt = *a.SystemPrompt
	}
	if a.IdentityMD != nil {
		cfg.IdentityMD = *a.IdentityMD
	}
	if a.SoulMD != nil {
		cfg.SoulMD = *a.SoulMD
	}
	if a.UserMD != nil {
		cfg.UserMD = *a.UserMD
	}
	return cfg
}

func toOpenAISSEChunk(chunk *burrow.StreamChunk, model string) map[string]any {
	switch chunk.Type {
	case "text":
		return map[string]any{
			"id":      "chatcmpl-" + uuid.New().String()[:8],
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]string{"content": chunk.Content},
				},
			},
		}
	case "error":
		return map[string]any{
			"error": map[string]string{"message": chunk.Error, "type": "server_error"},
		}
	case "done":
		return map[string]any{
			"id":      "chatcmpl-" + uuid.New().String()[:8],
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]any{
				{
					"index":         0,
					"delta":         map[string]string{},
					"finish_reason": "stop",
				},
			},
		}
	default:
		return map[string]any{}
	}
}

// logAudit is a helper that logs an audit event, ignoring errors.
func (s *Server) logAudit(r *http.Request, action, resourceType string, resourceID *uuid.UUID) {
	tenantID := getTenantID(r)
	userIDStr := getUserID(r)
	var actorID *uuid.UUID
	if uid, err := uuid.Parse(userIDStr); err == nil {
		actorID = &uid
	}

	reqID, _ := r.Context().Value(middleware.RequestIDCtxKey).(string)

	// Strip port from RemoteAddr for PostgreSQL inet type
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}

	event := &footprint.AuditEvent{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "user",
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		IPAddress:    ip,
		UserAgent:    r.UserAgent(),
		RequestID:    reqID,
	}

	if err := s.auditLogger.Log(r.Context(), event); err != nil {
		slog.Warn("failed to log audit event", "action", action, "error", err)
	}
}

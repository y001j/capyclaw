package burrow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"CapyClaw/internal/burrow/instinct"
	"CapyClaw/internal/burrow/llm"
	"CapyClaw/internal/burrow/mudbath"
	"CapyClaw/internal/burrow/nibble"
	"CapyClaw/internal/pond/memory"
	"CapyClaw/internal/pond/pebble"
	"CapyClaw/internal/shared/config"
)

// Stage is a single step in the agent execution pipeline.
type Stage interface {
	Name() string
	Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error)
}

// PipelineRequest carries data through the pipeline stages.
type PipelineRequest struct {
	AgentID     uuid.UUID
	TenantID    uuid.UUID
	Message     *IncomingMessage
	AgentConfig AgentConfig

	// Populated by stages
	Session        *Session
	SystemPrompt   string
	Messages       []Message
	ToolCalls      []ToolCall
	AvailableTools []llm.Tool // filtered tools available for LLM
	Model          string
	Response       *StreamChunk
	OutputCh       chan *StreamChunk // channel for streaming output from LLMInvoker

	// Populated after LLM response
	AssistantContent string
	Usage            *llm.Usage
}

// Message represents a conversation message in the pipeline.
type Message struct {
	Role        string       `json:"role"`
	Content     string       `json:"content"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`   // For assistant messages with tool_use blocks
	ToolResults []ToolResult `json:"tool_results,omitempty"` // For user messages with tool_result blocks
}

// ToolCall represents a tool invocation request.
type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// PipelineDeps holds the dependencies required by pipeline stages.
type PipelineDeps struct {
	SessionRepo     *pebble.SessionRepository
	MessageRepo     *pebble.MessageRepository
	LLMClient       llm.Client
	Config          *config.BurrowConfig
	ToolRegistry    *nibble.Registry
	ToolExecutor    *nibble.Executor
	MemoryMgr       *memory.Manager
	MemoryExtractor *memory.Extractor
	Compactor       *instinct.Compactor
	ContextMgr      *instinct.ContextManager
	Pool            *pgxpool.Pool
}

// Pipeline orchestrates the 8-stage agent execution pipeline.
type Pipeline struct {
	stages []Stage
}

// NewPipeline creates a new pipeline with the standard 8 stages.
func NewPipeline(deps *PipelineDeps) *Pipeline {
	defaultModel := "claude-sonnet-4-20250514"
	if deps.Config != nil && deps.Config.DefaultModel != "" {
		defaultModel = deps.Config.DefaultModel
	}

	maxToolIterations := 10
	if deps.Config != nil && deps.Config.ToolExecution.MaxIterations > 0 {
		maxToolIterations = deps.Config.ToolExecution.MaxIterations
	}

	return &Pipeline{
		stages: []Stage{
			&SessionResolver{sessionRepo: deps.SessionRepo},
			&WorkspaceLoader{pool: deps.Pool, memoryMgr: deps.MemoryMgr, toolRegistry: deps.ToolRegistry},
			&ModelSelector{defaultModel: defaultModel},
			&PromptBuilder{messageRepo: deps.MessageRepo},
			&ToolPolicyFilter{registry: deps.ToolRegistry},
			&LLMInvoker{
				client:     deps.LLMClient,
				compactor:  deps.Compactor,
				contextMgr: deps.ContextMgr,
			},
			&ToolExecutor{
				executor:  deps.ToolExecutor,
				llmClient: deps.LLMClient,
				maxIter:   maxToolIterations,
			},
			&SessionPersister{
				sessionRepo:     deps.SessionRepo,
				messageRepo:     deps.MessageRepo,
				memoryExtractor: deps.MemoryExtractor,
			},
		},
	}
}

// Execute runs the message through all pipeline stages and returns a streaming response.
func (p *Pipeline) Execute(ctx context.Context, req *PipelineRequest) (<-chan *StreamChunk, error) {
	ch := make(chan *StreamChunk, 64)
	req.OutputCh = ch

	go func() {
		defer close(ch)

		var err error
		for _, stage := range p.stages {
			slog.Debug("executing pipeline stage", "stage", stage.Name())
			req, err = stage.Process(ctx, req)
			if err != nil {
				ch <- &StreamChunk{
					Type:  "error",
					Error: fmt.Sprintf("pipeline stage %s failed: %v", stage.Name(), err),
				}
				return
			}
		}

		ch <- &StreamChunk{Type: "done"}
	}()

	return ch, nil
}

// ExecutePrompt runs a prompt through the pipeline and returns the full response text.
// This implements drift.PipelineExecutor for cron job execution.
func (p *Pipeline) ExecutePrompt(ctx context.Context, agentID, tenantID, prompt, sessionKey string) (string, error) {
	aid, err := uuid.Parse(agentID)
	if err != nil {
		return "", fmt.Errorf("invalid agent_id: %w", err)
	}
	tid, err := uuid.Parse(tenantID)
	if err != nil {
		return "", fmt.Errorf("invalid tenant_id: %w", err)
	}

	req := &PipelineRequest{
		AgentID:  aid,
		TenantID: tid,
		Message: &IncomingMessage{
			SessionKey: sessionKey,
			Content:    prompt,
			Role:       "user",
		},
	}

	chunks, err := p.Execute(ctx, req)
	if err != nil {
		return "", err
	}

	var result string
	for chunk := range chunks {
		switch chunk.Type {
		case "text":
			result += chunk.Content
		case "error":
			return result, fmt.Errorf("pipeline error: %s", chunk.Error)
		}
	}
	return result, nil
}

// --- Stage implementations ---

// SessionResolver determines or creates the session from channel context.
type SessionResolver struct {
	sessionRepo *pebble.SessionRepository
}

func (s *SessionResolver) Name() string { return "session_resolver" }

func (s *SessionResolver) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
	if s.sessionRepo == nil {
		// No repo available, create an in-memory session
		req.Session = &Session{
			ID:         uuid.New(),
			AgentID:    req.AgentID,
			TenantID:   req.TenantID,
			SessionKey: req.Message.SessionKey,
			Status:     "active",
		}
		return req, nil
	}

	sessionKey := req.Message.SessionKey
	if sessionKey == "" {
		sessionKey = uuid.New().String()
	}

	dbSession, err := s.sessionRepo.GetOrCreate(ctx, req.AgentID, req.TenantID, sessionKey)
	if err != nil {
		return nil, fmt.Errorf("resolving session: %w", err)
	}

	req.Session = &Session{
		ID:                dbSession.ID,
		AgentID:           dbSession.AgentID,
		TenantID:          dbSession.TenantID,
		SessionKey:        dbSession.SessionKey,
		CompactionCount:   dbSession.CompactionCount,
		TotalInputTokens:  dbSession.TotalInputTokens,
		TotalOutputTokens: dbSession.TotalOutputTokens,
		TotalCostUSD:      dbSession.TotalCostUSD,
		Status:            dbSession.Status,
	}
	if dbSession.ContextSummary != nil {
		req.Session.ContextSummary = *dbSession.ContextSummary
	}

	return req, nil
}

// WorkspaceLoader loads the agent's identity, skills, and boot files.
type WorkspaceLoader struct {
	pool         *pgxpool.Pool
	memoryMgr    *memory.Manager
	toolRegistry *nibble.Registry
}

func (s *WorkspaceLoader) Name() string { return "workspace_loader" }
func (s *WorkspaceLoader) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
	// Load skills from database if pool is available
	if s.pool != nil {
		rows, err := s.pool.Query(ctx, `
			SELECT s.name, s.description, s.content_md, s.manifest, s.sandbox_tier
			FROM skills s
			JOIN agent_skills ask ON s.id = ask.skill_id
			WHERE ask.agent_id = $1 AND ask.enabled = true
			ORDER BY ask.priority DESC
		`, req.AgentID)
		if err != nil {
			slog.Warn("failed to load agent skills", "error", err)
		} else {
			defer rows.Close()
			var skills []instinct.SkillPrompt
			for rows.Next() {
				var sp instinct.SkillPrompt
				var desc, content *string
				var manifestJSON []byte
				var sandboxTier string
				if err := rows.Scan(&sp.Name, &desc, &content, &manifestJSON, &sandboxTier); err != nil {
					slog.Warn("failed to scan skill", "error", err)
					continue
				}
				if desc != nil {
					sp.Description = *desc
				}
				if content != nil {
					sp.Content = *content
				}
				skills = append(skills, sp)

				// Parse manifest and register tools dynamically
				if s.toolRegistry != nil && len(manifestJSON) > 0 {
					var manifest struct {
						Tools []struct {
							Name        string         `json:"name"`
							Description string         `json:"description"`
							Parameters  map[string]any `json:"parameters"`
							Handler     string         `json:"handler"` // "builtin" for in-process handlers
						} `json:"tools"`
					}
					if json.Unmarshal(manifestJSON, &manifest) == nil {
						for _, t := range manifest.Tools {
							source := "skill"
							if t.Handler == "builtin" {
								// Only register if a builtin handler is actually available
								if _, ok := nibble.GetBuiltin(t.Name); !ok {
									slog.Debug("skipping skill tool (no builtin handler)", "tool", t.Name, "skill", sp.Name)
									continue
								}
								source = "bundled"
							}
							s.toolRegistry.Register(&nibble.ToolDef{
								Name:        t.Name,
								Description: t.Description,
								Parameters:  t.Parameters,
								SandboxTier: nibble.SandboxTier(sandboxTier),
								Source:      source,
							})
							slog.Debug("registered skill tool", "tool", t.Name, "skill", sp.Name)
						}
					}
				}
			}
			req.AgentConfig.Skills = skills
		}
	}

	// Load relevant memories if manager is available
	if s.memoryMgr != nil && req.Message != nil {
		slog.Debug("recalling memories", "agent_id", req.AgentID, "query", req.Message.Content)
		memories, err := s.memoryMgr.Recall(ctx, req.AgentID, req.Message.Content, 10)
		if err != nil {
			slog.Warn("failed to recall memories", "error", err)
		} else if len(memories) > 0 {
			slog.Info("memories recalled", "count", len(memories))
			var memoryContext string
			for _, m := range memories {
				memoryContext += fmt.Sprintf("- [%s] %s\n", m.MemoryType, m.Content)
			}
			req.AgentConfig.MemoryContext = memoryContext
		}
	}

	return req, nil
}

// ModelSelector resolves the model, auth profile, and fallback chain.
type ModelSelector struct {
	defaultModel string
}

func (s *ModelSelector) Name() string { return "model_selector" }
func (s *ModelSelector) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
	req.Model = req.AgentConfig.Model
	if req.Model == "" {
		req.Model = s.defaultModel
	}
	return req, nil
}

// PromptBuilder assembles the system prompt from identity, skills XML, and memory.
type PromptBuilder struct {
	messageRepo *pebble.MessageRepository
}

func (s *PromptBuilder) Name() string { return "prompt_builder" }
func (s *PromptBuilder) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
	// Build system prompt using instinct module
	builder := &instinct.PromptBuilder{}
	prompt, err := builder.Build(ctx, &instinct.BuildRequest{
		IdentityMD:     req.AgentConfig.IdentityMD,
		SoulMD:         req.AgentConfig.SoulMD,
		UserMD:         req.AgentConfig.UserMD,
		Skills:         req.AgentConfig.Skills,
		ContextSummary: req.Session.ContextSummary,
		MemoryContext:  req.AgentConfig.MemoryContext,
	})
	if err != nil {
		return nil, fmt.Errorf("building prompt: %w", err)
	}
	req.SystemPrompt = prompt

	// Load history messages from database
	if s.messageRepo != nil && req.Session.ID != uuid.Nil {
		dbMessages, err := s.messageRepo.ListBySession(ctx, req.Session.ID, 20, 0)
		if err != nil {
			slog.Warn("failed to load message history", "error", err)
		} else {
			for _, m := range dbMessages {
				var content string
				// Try to unmarshal as a quoted string first, then as raw
				if err := json.Unmarshal(m.Content, &content); err != nil {
					content = string(m.Content)
				}
				req.Messages = append(req.Messages, Message{
					Role:    m.Role,
					Content: content,
				})
			}
		}
	}

	// Append current user message
	req.Messages = append(req.Messages, Message{
		Role:    req.Message.Role,
		Content: req.Message.Content,
	})

	return req, nil
}

// ToolPolicyFilter applies cascading tool policy (global → tenant → agent → channel).
type ToolPolicyFilter struct {
	registry *nibble.Registry
}

func (s *ToolPolicyFilter) Name() string { return "tool_policy_filter" }
func (s *ToolPolicyFilter) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
	if s.registry == nil {
		return req, nil
	}

	allTools := s.registry.List()
	if len(allTools) == 0 {
		return req, nil
	}

	// Build the agent's tool policy from config
	agentPolicy := nibble.ToolPolicy{
		DefaultAction: nibble.PolicyAllow,
		ToolOverrides: make(map[string]nibble.PolicyAction),
	}
	for tool, action := range req.AgentConfig.ToolPolicy {
		if tool == "default" {
			agentPolicy.DefaultAction = nibble.PolicyAction(action)
		} else {
			agentPolicy.ToolOverrides[tool] = nibble.PolicyAction(action)
		}
	}

	// Filter tools by policy
	var allowedTools []llm.Tool
	for _, toolDef := range allTools {
		action := nibble.EvaluatePolicy(toolDef.Name, agentPolicy)
		if action == nibble.PolicyDeny {
			continue
		}
		allowedTools = append(allowedTools, llm.Tool{
			Name:        toolDef.Name,
			Description: toolDef.Description,
			Parameters:  toolDef.Parameters,
		})
	}

	req.AvailableTools = allowedTools
	slog.Info("tools available for LLM", "count", len(allowedTools))
	return req, nil
}

// LLMInvoker streams the request to the selected LLM provider.
// Before sending, it estimates token count and compacts history if needed.
type LLMInvoker struct {
	client     llm.Client
	compactor  *instinct.Compactor
	contextMgr *instinct.ContextManager
}

func (s *LLMInvoker) Name() string { return "llm_invoker" }
func (s *LLMInvoker) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
	if s.client == nil {
		return nil, fmt.Errorf("no LLM provider configured")
	}

	// Build LLM request
	llmMessages := make([]llm.Message, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		llmMessages = append(llmMessages, llm.Message{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}
	for _, m := range req.Messages {
		llmMsg := llm.Message{
			Role:    m.Role,
			Content: m.Content,
		}
		for _, tc := range m.ToolCalls {
			llmMsg.ToolCalls = append(llmMsg.ToolCalls, llm.ToolCall{
				ID: tc.ID, Name: tc.Name, Input: tc.Input,
			})
		}
		for _, tr := range m.ToolResults {
			llmMsg.ToolResults = append(llmMsg.ToolResults, llm.ToolResult{
				ToolUseID: tr.ToolUseID, Content: tr.Content,
			})
		}
		llmMessages = append(llmMessages, llmMsg)
	}

	// Compact history if estimated tokens exceed context window threshold
	if s.contextMgr != nil && s.compactor != nil && len(llmMessages) > 3 {
		estimatedTokens := estimateTokens(llmMessages)
		if s.contextMgr.ShouldCompact(estimatedTokens) {
			slog.Info("compacting history before LLM call",
				"estimated_tokens", estimatedTokens,
				"message_count", len(llmMessages),
			)
			llmMessages = s.compactMessages(ctx, llmMessages)
		}
	}

	llmReq := &llm.Request{
		Model:    req.Model,
		Messages: llmMessages,
		Tools:    req.AvailableTools,
		Stream:   true,
	}

	chunks, err := s.client.Stream(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("LLM stream: %w", err)
	}

	// Forward chunks to output channel and collect assistant content
	var assistantContent string
	var usage *llm.Usage

	for chunk := range chunks {
		switch chunk.Type {
		case "text":
			assistantContent += chunk.Content
			// Forward to output channel
			if req.OutputCh != nil {
				req.OutputCh <- &StreamChunk{
					Type:    "text",
					Content: chunk.Content,
				}
			}
		case "tool_call":
			if chunk.ToolCall != nil {
				req.ToolCalls = append(req.ToolCalls, ToolCall{
					ID:    chunk.ToolCall.ID,
					Name:  chunk.ToolCall.Name,
					Input: chunk.ToolCall.Input,
				})
			}
		case "done":
			// Will be sent by Pipeline.Execute
		case "error":
			return nil, fmt.Errorf("LLM error: %s", chunk.Content)
		}

		if chunk.Usage != nil {
			if usage == nil {
				usage = &llm.Usage{}
			}
			if chunk.Usage.InputTokens > 0 {
				usage.InputTokens = chunk.Usage.InputTokens
			}
			if chunk.Usage.OutputTokens > 0 {
				usage.OutputTokens = chunk.Usage.OutputTokens
			}
		}
	}

	req.AssistantContent = assistantContent
	req.Usage = usage
	return req, nil
}

// ToolExecutor executes tool calls in the appropriate sandbox tier.
type ToolExecutor struct {
	executor  *nibble.Executor
	llmClient llm.Client
	maxIter   int
}

func (s *ToolExecutor) Name() string { return "tool_executor" }
func (s *ToolExecutor) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
	if len(req.ToolCalls) == 0 || s.executor == nil {
		return req, nil
	}

	for iteration := 0; iteration < s.maxIter; iteration++ {
		slog.Info("tool execution iteration", "iteration", iteration, "tool_count", len(req.ToolCalls))

		// Add ONE assistant message containing ALL tool_use blocks
		// (Anthropic API requires all tool_use in a single assistant message)
		assistantMsg := Message{
			Role:    "assistant",
			Content: req.AssistantContent,
		}
		// Store tool calls as structured data for the Anthropic provider
		assistantMsg.ToolCalls = make([]ToolCall, len(req.ToolCalls))
		copy(assistantMsg.ToolCalls, req.ToolCalls)
		req.Messages = append(req.Messages, assistantMsg)
		req.AssistantContent = ""

		// Execute each tool call and collect results
		var toolResults []ToolResult
		for _, tc := range req.ToolCalls {
			toolDef, ok := s.executor.Registry().Get(tc.Name)

			var output string
			if !ok {
				output = fmt.Sprintf("Error: tool not found: %s", tc.Name)
			} else {
				tier := mudbath.SelectTier(toolDef.Source, toolDef.Source == "bundled")

				var input map[string]any
				if err := json.Unmarshal([]byte(tc.Input), &input); err != nil {
					input = map[string]any{"raw": tc.Input}
				}
				// Inject agent context for tools that need it
				input["_agent_id"] = req.AgentID.String()
				nibbleTC := &nibble.ToolCall{
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				}

				result, err := s.executor.Execute(ctx, nibbleTC, nibble.SandboxTier(tierToString(tier)))
				if err != nil {
					output = fmt.Sprintf("Error: %s", err.Error())
				} else {
					output = result.Output
					if result.Error != "" {
						output = fmt.Sprintf("Error: %s", result.Error)
					}
				}
			}

			if req.OutputCh != nil {
				req.OutputCh <- &StreamChunk{
					Type:    "tool_result",
					Content: output,
				}
			}

			toolResults = append(toolResults, ToolResult{
				ToolUseID: tc.ID,
				Content:   output,
			})
		}

		// Add ONE user message containing ALL tool_result blocks
		req.Messages = append(req.Messages, Message{
			Role:        "tool",
			ToolResults: toolResults,
		})

		req.ToolCalls = nil

		// Call LLM again with tool results
		if s.llmClient == nil {
			break
		}

		llmMessages := make([]llm.Message, 0, len(req.Messages)+1)
		if req.SystemPrompt != "" {
			llmMessages = append(llmMessages, llm.Message{Role: "system", Content: req.SystemPrompt})
		}
		for _, m := range req.Messages {
			llmMsg := llm.Message{
				Role:    m.Role,
				Content: m.Content,
			}
			for _, tc := range m.ToolCalls {
				llmMsg.ToolCalls = append(llmMsg.ToolCalls, llm.ToolCall{
					ID: tc.ID, Name: tc.Name, Input: tc.Input,
				})
			}
			for _, tr := range m.ToolResults {
				llmMsg.ToolResults = append(llmMsg.ToolResults, llm.ToolResult{
					ToolUseID: tr.ToolUseID, Content: tr.Content,
				})
			}
			llmMessages = append(llmMessages, llmMsg)
		}

		llmReq := &llm.Request{
			Model:    req.Model,
			Messages: llmMessages,
			Tools:    req.AvailableTools,
			Stream:   true,
		}

		chunks, err := s.llmClient.Stream(ctx, llmReq)
		if err != nil {
			return nil, fmt.Errorf("LLM stream in tool loop: %w", err)
		}

		var assistantContent string
		for chunk := range chunks {
			switch chunk.Type {
			case "text":
				assistantContent += chunk.Content
				if req.OutputCh != nil {
					req.OutputCh <- &StreamChunk{Type: "text", Content: chunk.Content}
				}
			case "tool_call":
				if chunk.ToolCall != nil {
					req.ToolCalls = append(req.ToolCalls, ToolCall{
						ID:    chunk.ToolCall.ID,
						Name:  chunk.ToolCall.Name,
						Input: chunk.ToolCall.Input,
					})
				}
			case "error":
				return nil, fmt.Errorf("LLM error in tool loop: %s", chunk.Content)
			}

			if chunk.Usage != nil {
				if req.Usage == nil {
					req.Usage = &llm.Usage{}
				}
				if chunk.Usage.InputTokens > 0 {
					req.Usage.InputTokens += chunk.Usage.InputTokens
				}
				if chunk.Usage.OutputTokens > 0 {
					req.Usage.OutputTokens += chunk.Usage.OutputTokens
				}
			}
		}

		req.AssistantContent = assistantContent

		// If no more tool calls, break out of the loop
		if len(req.ToolCalls) == 0 {
			break
		}
	}

	return req, nil
}

// tierToString converts a mudbath.Tier to the nibble SandboxTier string.
func tierToString(t mudbath.Tier) string {
	switch t {
	case mudbath.TierWASM:
		return "wasm"
	case mudbath.TierGVisor:
		return "gvisor"
	case mudbath.TierFirecracker:
		return "firecracker"
	default:
		return "wasm"
	}
}

func mustMarshal(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `""`
	}
	return string(data)
}

// SessionPersister writes messages to DB, updates token counts, and triggers memory flush.
type SessionPersister struct {
	sessionRepo     *pebble.SessionRepository
	messageRepo     *pebble.MessageRepository
	memoryExtractor *memory.Extractor
}

func (s *SessionPersister) Name() string { return "session_persister" }
func (s *SessionPersister) Process(ctx context.Context, req *PipelineRequest) (*PipelineRequest, error) {
	if s.messageRepo == nil || req.Session == nil {
		return req, nil
	}

	// Persist user message
	userContentJSON, _ := json.Marshal(req.Message.Content)
	userMsg := &pebble.Message{
		SessionID: req.Session.ID,
		TenantID:  req.TenantID,
		Role:      "user",
		Content:   userContentJSON,
	}
	if err := s.messageRepo.Append(ctx, userMsg); err != nil {
		slog.Warn("failed to persist user message", "error", err)
	}

	// Persist assistant response
	if req.AssistantContent != "" {
		assistantContentJSON, _ := json.Marshal(req.AssistantContent)
		model := req.Model
		assistantMsg := &pebble.Message{
			SessionID: req.Session.ID,
			TenantID:  req.TenantID,
			Role:      "assistant",
			Content:   assistantContentJSON,
			Model:     &model,
		}

		// Add usage if available
		if req.Usage != nil {
			usageJSON, _ := json.Marshal(req.Usage)
			assistantMsg.Usage = usageJSON
		}

		if err := s.messageRepo.Append(ctx, assistantMsg); err != nil {
			slog.Warn("failed to persist assistant message", "error", err)
		}
	}

	// Update session token counts
	if s.sessionRepo != nil && req.Usage != nil {
		if err := s.sessionRepo.UpdateTokenCounts(ctx, req.Session.ID,
			int64(req.Usage.InputTokens), int64(req.Usage.OutputTokens)); err != nil {
			slog.Warn("failed to update session token counts", "error", err)
		}
	}

	// Trigger async memory extraction
	if s.memoryExtractor != nil && req.Message.Content != "" && req.AssistantContent != "" {
		s.memoryExtractor.ExtractAsync(memory.ExtractionInput{
			AgentID:      req.AgentID,
			TenantID:     req.TenantID,
			SessionID:    req.Session.ID,
			UserMsg:      req.Message.Content,
			AssistantMsg: req.AssistantContent,
		})
	}

	return req, nil
}

// estimateTokens provides a rough token count estimate.
// Uses 3 chars/token as a conservative estimate for mixed English/CJK content.
func estimateTokens(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 3
		for _, tc := range m.ToolCalls {
			total += len(tc.Input) / 3
		}
		for _, tr := range m.ToolResults {
			total += len(tr.Content) / 3
		}
	}
	return total
}

// compactMessages summarizes older history messages while keeping system prompt and recent messages.
func (s *LLMInvoker) compactMessages(ctx context.Context, messages []llm.Message) []llm.Message {
	// Keep: system prompt (first) + recent messages (last 4 = ~2 rounds)
	// Compact: everything in between
	if len(messages) <= 5 {
		return messages
	}

	systemMsg := messages[0]
	recentCount := 4
	if len(messages)-1 < recentCount {
		recentCount = len(messages) - 1
	}
	oldMessages := messages[1 : len(messages)-recentCount]
	recentMessages := messages[len(messages)-recentCount:]

	// Build text for summarization
	var texts []string
	for _, m := range oldMessages {
		texts = append(texts, fmt.Sprintf("%s: %s", m.Role, m.Content))
	}

	resultCh := s.compactor.CompactAsync(ctx, "", texts)
	result := <-resultCh
	if result == nil || result.Error != nil {
		if result != nil {
			slog.Warn("compaction failed, using original messages", "error", result.Error)
		}
		return messages
	}

	slog.Info("history compacted before LLM call",
		"original_messages", len(oldMessages),
		"tokens_saved", result.TokensSaved,
	)

	// Rebuild: system + summary + recent
	compacted := make([]llm.Message, 0, 2+recentCount)
	compacted = append(compacted, systemMsg)
	compacted = append(compacted, llm.Message{
		Role:    "user",
		Content: "[Previous conversation summary]\n" + result.Summary,
	})
	compacted = append(compacted, recentMessages...)
	return compacted
}

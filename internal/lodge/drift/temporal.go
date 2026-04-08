package drift

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const (
	// AgentTaskQueue is the Temporal task queue for agent workflows.
	AgentTaskQueue = "capyclaw-agent-tasks"

	// WorkflowTypeAgentTask is the workflow name for agent task execution.
	WorkflowTypeAgentTask = "AgentTaskWorkflow"
)

// TemporalClient wraps the Temporal SDK client for durable workflow execution.
type TemporalClient struct {
	client    client.Client
	namespace string
	taskQueue string
}

// NewTemporalClient creates a new Temporal client.
func NewTemporalClient(host, namespace, taskQueue string) (*TemporalClient, error) {
	c, err := client.Dial(client.Options{
		HostPort:  host,
		Namespace: namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to Temporal: %w", err)
	}

	if taskQueue == "" {
		taskQueue = AgentTaskQueue
	}

	return &TemporalClient{
		client:    c,
		namespace: namespace,
		taskQueue: taskQueue,
	}, nil
}

// StartWorkflow starts a new Temporal workflow.
func (tc *TemporalClient) StartWorkflow(ctx context.Context, workflowID string, wf any, args ...any) (string, error) {
	opts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: tc.taskQueue,
	}

	run, err := tc.client.ExecuteWorkflow(ctx, opts, wf, args...)
	if err != nil {
		return "", fmt.Errorf("starting workflow: %w", err)
	}

	return run.GetRunID(), nil
}

// StartAgentTask starts an agent task workflow.
func (tc *TemporalClient) StartAgentTask(ctx context.Context, input AgentTaskInput) (string, error) {
	workflowID := fmt.Sprintf("agent-task-%s-%d", input.AgentID, time.Now().UnixNano())

	opts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: tc.taskQueue,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}

	run, err := tc.client.ExecuteWorkflow(ctx, opts, AgentTaskWorkflow, input)
	if err != nil {
		return "", fmt.Errorf("starting agent task workflow: %w", err)
	}

	slog.Info("started agent task workflow",
		"workflow_id", workflowID,
		"run_id", run.GetRunID(),
		"agent_id", input.AgentID,
	)

	return run.GetRunID(), nil
}

// GetWorkflowResult waits for a workflow to complete and returns the result.
func (tc *TemporalClient) GetWorkflowResult(ctx context.Context, workflowID, runID string) (string, error) {
	run := tc.client.GetWorkflow(ctx, workflowID, runID)
	var result string
	if err := run.Get(ctx, &result); err != nil {
		return "", fmt.Errorf("getting workflow result: %w", err)
	}
	return result, nil
}

// Close closes the Temporal client.
func (tc *TemporalClient) Close() {
	tc.client.Close()
}

// --- Workflow Definitions ---

// AgentTaskInput is the input for an agent task workflow.
type AgentTaskInput struct {
	AgentID   string `json:"agent_id"`
	TenantID  string `json:"tenant_id"`
	Prompt    string `json:"prompt"`
	SessionKey string `json:"session_key"`
	MaxRetries int    `json:"max_retries"`
}

// AgentTaskOutput is the output of an agent task workflow.
type AgentTaskOutput struct {
	Response   string   `json:"response"`
	ToolCalls  []string `json:"tool_calls,omitempty"`
	TokensUsed int      `json:"tokens_used"`
}

// PrepareContextResult is the output of the context preparation activity.
type PrepareContextResult struct {
	SystemPrompt string   `json:"system_prompt"`
	Messages     []string `json:"messages"`
	Model        string   `json:"model"`
}

// LLMInvokeResult is the output of the LLM invocation activity.
type LLMInvokeResult struct {
	Response   string   `json:"response"`
	ToolCalls  []ToolCallInfo `json:"tool_calls,omitempty"`
	TokensUsed int      `json:"tokens_used"`
}

// ToolCallInfo represents a tool call from the LLM.
type ToolCallInfo struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolExecuteResult is the output of tool execution activity.
type ToolExecuteResult struct {
	Results []ToolResultInfo `json:"results"`
}

// ToolResultInfo is the result of a single tool execution.
type ToolResultInfo struct {
	Name   string `json:"name"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// AgentTaskWorkflow is the main durable agent task workflow.
// It orchestrates: context preparation → LLM invocation → tool execution loop → persistence.
func AgentTaskWorkflow(ctx workflow.Context, input AgentTaskInput) (string, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("starting agent task workflow",
		"agent_id", input.AgentID,
	)

	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	// Step 1: Prepare context (load agent config, session history, etc.)
	var prepResult PrepareContextResult
	if err := workflow.ExecuteActivity(ctx, PrepareContextActivity, input).Get(ctx, &prepResult); err != nil {
		return "", fmt.Errorf("preparing context: %w", err)
	}

	// Step 2: LLM invocation loop (with tool calls)
	maxIterations := 10
	var finalResponse string

	for i := 0; i < maxIterations; i++ {
		// Invoke LLM
		var llmResult LLMInvokeResult
		if err := workflow.ExecuteActivity(ctx, InvokeLLMActivity, input, prepResult).Get(ctx, &llmResult); err != nil {
			return "", fmt.Errorf("invoking LLM (iteration %d): %w", i, err)
		}

		finalResponse = llmResult.Response

		// If no tool calls, we're done
		if len(llmResult.ToolCalls) == 0 {
			break
		}

		// Execute tools
		toolOpts := workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Minute,
			HeartbeatTimeout:    30 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 2,
			},
		}
		toolCtx := workflow.WithActivityOptions(ctx, toolOpts)

		var toolResult ToolExecuteResult
		if err := workflow.ExecuteActivity(toolCtx, ExecuteToolsActivity, llmResult.ToolCalls).Get(ctx, &toolResult); err != nil {
			return "", fmt.Errorf("executing tools (iteration %d): %w", i, err)
		}

		logger.Info("tool execution completed",
			"iteration", i,
			"tools_executed", len(toolResult.Results),
		)
	}

	// Step 3: Persist results
	if err := workflow.ExecuteActivity(ctx, PersistResultActivity, input, finalResponse).Get(ctx, nil); err != nil {
		logger.Warn("failed to persist result, but returning response", "error", err)
	}

	return finalResponse, nil
}

// --- Activity Definitions ---
// These are implemented as functions that will be registered with the Temporal worker.
// The actual implementations are provided by the application layer.

// PrepareContextActivity loads agent configuration and session context.
func PrepareContextActivity(ctx context.Context, input AgentTaskInput) (*PrepareContextResult, error) {
	// This will be implemented by the application layer that has access
	// to the database and agent configuration.
	slog.Info("preparing context for agent task",
		"agent_id", input.AgentID,
		"session_key", input.SessionKey,
	)

	return &PrepareContextResult{
		Model: "claude-sonnet-4-20250514",
	}, nil
}

// InvokeLLMActivity calls the LLM provider with the prepared context.
func InvokeLLMActivity(ctx context.Context, input AgentTaskInput, prep PrepareContextResult) (*LLMInvokeResult, error) {
	slog.Info("invoking LLM for agent task",
		"agent_id", input.AgentID,
		"model", prep.Model,
	)

	// This will be implemented by the application layer with access to LLM providers.
	return &LLMInvokeResult{
		Response: "Activity not yet connected to LLM provider",
	}, nil
}

// ExecuteToolsActivity runs tool calls in the appropriate sandbox.
func ExecuteToolsActivity(ctx context.Context, toolCalls []ToolCallInfo) (*ToolExecuteResult, error) {
	slog.Info("executing tools", "count", len(toolCalls))

	results := make([]ToolResultInfo, len(toolCalls))
	for i, tc := range toolCalls {
		results[i] = ToolResultInfo{
			Name:   tc.Name,
			Output: "Activity not yet connected to tool executor",
		}
	}

	return &ToolExecuteResult{Results: results}, nil
}

// PersistResultActivity saves the workflow result to the database.
func PersistResultActivity(ctx context.Context, input AgentTaskInput, response string) error {
	slog.Info("persisting agent task result",
		"agent_id", input.AgentID,
		"response_length", len(response),
	)
	// This will be implemented by the application layer.
	return nil
}

// --- Worker Setup ---

// TemporalWorker manages the Temporal worker that processes agent workflows.
type TemporalWorker struct {
	worker worker.Worker
}

// NewTemporalWorker creates and configures a Temporal worker.
func NewTemporalWorker(tc *TemporalClient) *TemporalWorker {
	w := worker.New(tc.client, tc.taskQueue, worker.Options{
		MaxConcurrentActivityExecutionSize: 10,
		MaxConcurrentWorkflowTaskExecutionSize: 5,
	})

	// Register workflows
	w.RegisterWorkflow(AgentTaskWorkflow)

	// Register activities
	w.RegisterActivity(PrepareContextActivity)
	w.RegisterActivity(InvokeLLMActivity)
	w.RegisterActivity(ExecuteToolsActivity)
	w.RegisterActivity(PersistResultActivity)

	return &TemporalWorker{worker: w}
}

// Start begins processing Temporal tasks.
func (tw *TemporalWorker) Start() error {
	return tw.worker.Start()
}

// Stop gracefully shuts down the worker.
func (tw *TemporalWorker) Stop() {
	tw.worker.Stop()
}

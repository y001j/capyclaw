package herd

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

// WorkflowType defines the coordination pattern.
type WorkflowType string

const (
	WorkflowSequential WorkflowType = "sequential" // Execute agents one after another
	WorkflowParallel   WorkflowType = "parallel"   // Fan-out / fan-in
	WorkflowSupervisor WorkflowType = "supervisor"  // Supervisor assigns and aggregates
)

// MultiAgentWorkflowInput defines the input for a multi-agent workflow.
type MultiAgentWorkflowInput struct {
	TenantID     string            `json:"tenant_id"`
	Agents       []AgentAssignment `json:"agents"`
	Coordination WorkflowType      `json:"coordination"`
	Prompt       string            `json:"prompt"`
}

// AgentAssignment maps an agent to a role in the workflow.
type AgentAssignment struct {
	AgentSlug string `json:"agent_slug"`
	Role      string `json:"role"`
	Prompt    string `json:"prompt"`
	DependsOn []int  `json:"depends_on,omitempty"` // indices of prerequisite agents
}

// WorkflowResult contains the outcome of a multi-agent workflow.
type WorkflowResult struct {
	Results []AgentResult `json:"results"`
	Error   string        `json:"error,omitempty"`
}

// AgentResult contains a single agent's contribution to the workflow.
type AgentResult struct {
	AgentSlug string `json:"agent_slug"`
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
}

// WorkflowExecutor runs multi-agent workflows with different coordination patterns.
type WorkflowExecutor struct {
	manager *Manager
	router  *MessageRouter
}

// NewWorkflowExecutor creates a new workflow executor.
func NewWorkflowExecutor(manager *Manager, router *MessageRouter) *WorkflowExecutor {
	return &WorkflowExecutor{
		manager: manager,
		router:  router,
	}
}

// Execute runs a multi-agent workflow.
func (w *WorkflowExecutor) Execute(ctx context.Context, parentID uuid.UUID, input *MultiAgentWorkflowInput) (*WorkflowResult, error) {
	slog.Info("executing workflow",
		"coordination", input.Coordination,
		"agents_count", len(input.Agents),
	)

	switch input.Coordination {
	case WorkflowSequential:
		return w.executeSequential(ctx, parentID, input)
	case WorkflowParallel:
		return w.executeParallel(ctx, parentID, input)
	case WorkflowSupervisor:
		return w.executeSupervisor(ctx, parentID, input)
	default:
		return nil, fmt.Errorf("unknown workflow type: %s", input.Coordination)
	}
}

// executeSequential runs agents one after another, passing each result to the next.
func (w *WorkflowExecutor) executeSequential(ctx context.Context, parentID uuid.UUID, input *MultiAgentWorkflowInput) (*WorkflowResult, error) {
	result := &WorkflowResult{
		Results: make([]AgentResult, 0, len(input.Agents)),
	}

	var prevOutput string
	for _, assignment := range input.Agents {
		prompt := assignment.Prompt
		if prevOutput != "" {
			prompt = fmt.Sprintf("Previous agent output:\n%s\n\nYour task:\n%s", prevOutput, assignment.Prompt)
		}

		output, err := w.router.Delegate(ctx, parentID, assignment.AgentSlug, prompt)
		agentResult := AgentResult{
			AgentSlug: assignment.AgentSlug,
			Output:    output,
		}
		if err != nil {
			agentResult.Error = err.Error()
			result.Results = append(result.Results, agentResult)
			result.Error = fmt.Sprintf("agent %s failed: %v", assignment.AgentSlug, err)
			return result, nil
		}

		result.Results = append(result.Results, agentResult)
		prevOutput = output
	}

	return result, nil
}

// executeParallel runs all agents concurrently and collects results.
func (w *WorkflowExecutor) executeParallel(ctx context.Context, parentID uuid.UUID, input *MultiAgentWorkflowInput) (*WorkflowResult, error) {
	result := &WorkflowResult{
		Results: make([]AgentResult, len(input.Agents)),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i, assignment := range input.Agents {
		wg.Add(1)
		go func(idx int, a AgentAssignment) {
			defer wg.Done()

			output, err := w.router.Delegate(ctx, parentID, a.AgentSlug, a.Prompt)
			agentResult := AgentResult{
				AgentSlug: a.AgentSlug,
				Output:    output,
			}
			if err != nil {
				agentResult.Error = err.Error()
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}

			mu.Lock()
			result.Results[idx] = agentResult
			mu.Unlock()
		}(i, assignment)
	}

	wg.Wait()

	if firstErr != nil {
		result.Error = firstErr.Error()
	}

	return result, nil
}

// executeSupervisor uses the first agent as a supervisor that assigns tasks to workers.
// The supervisor's prompt is augmented with the available worker slugs.
func (w *WorkflowExecutor) executeSupervisor(ctx context.Context, parentID uuid.UUID, input *MultiAgentWorkflowInput) (*WorkflowResult, error) {
	if len(input.Agents) < 2 {
		return nil, fmt.Errorf("supervisor workflow requires at least 2 agents")
	}

	supervisor := input.Agents[0]
	workers := input.Agents[1:]

	// Build worker list for the supervisor
	workerList := "Available workers:\n"
	for _, w := range workers {
		workerList += fmt.Sprintf("- %s (role: %s)\n", w.AgentSlug, w.Role)
	}

	supervisorPrompt := fmt.Sprintf("%s\n\n%s\n\nOriginal task:\n%s",
		supervisor.Prompt, workerList, input.Prompt)

	// Run supervisor to get the plan
	planOutput, err := w.router.Delegate(ctx, parentID, supervisor.AgentSlug, supervisorPrompt)
	if err != nil {
		return &WorkflowResult{
			Error: fmt.Sprintf("supervisor failed: %v", err),
		}, nil
	}

	// Execute workers in parallel with the supervisor's plan
	result := &WorkflowResult{
		Results: []AgentResult{
			{AgentSlug: supervisor.AgentSlug, Output: planOutput},
		},
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, worker := range workers {
		wg.Add(1)
		go func(a AgentAssignment) {
			defer wg.Done()

			workerPrompt := fmt.Sprintf("Supervisor's plan:\n%s\n\nYour role: %s\nYour task:\n%s",
				planOutput, a.Role, a.Prompt)

			output, err := w.router.Delegate(ctx, parentID, a.AgentSlug, workerPrompt)
			agentResult := AgentResult{
				AgentSlug: a.AgentSlug,
				Output:    output,
			}
			if err != nil {
				agentResult.Error = err.Error()
			}

			mu.Lock()
			result.Results = append(result.Results, agentResult)
			mu.Unlock()
		}(worker)
	}

	wg.Wait()

	return result, nil
}

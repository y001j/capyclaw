package nibble

import (
	"context"
	"encoding/json"
	"fmt"
)

// CronFunc types for cron operations.
type (
	CronCreateFunc func(ctx context.Context, agentID, name, schedule, action string) (string, error)
	CronListFunc   func(ctx context.Context, agentID string) ([]map[string]any, error)
	CronDeleteFunc func(ctx context.Context, agentID, cronID string) error
)

var (
	cronCreateFn CronCreateFunc
	cronListFn   CronListFunc
	cronDeleteFn CronDeleteFunc
)

// SetCronFuncs sets the cron operation functions.
func SetCronFuncs(create CronCreateFunc, list CronListFunc, del CronDeleteFunc) {
	cronCreateFn = create
	cronListFn = list
	cronDeleteFn = del
}

// registerCronTools registers scheduled task management tools.
// group:automation — cron_create, cron_list, cron_delete
func registerCronTools(r *Registry) {
	r.Register(&ToolDef{
		Name:        "cron_create",
		Description: "Create a scheduled task that runs at specified intervals. Uses cron expression syntax (e.g., '0 9 * * *' for daily at 9am).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "A descriptive name for the cron job",
				},
				"schedule": map[string]any{
					"type":        "string",
					"description": "Cron expression (e.g., '*/5 * * * *' for every 5 minutes)",
				},
				"action": map[string]any{
					"type":        "string",
					"description": "The action to perform (a message to send to the agent when triggered)",
				},
			},
			"required": []string{"name", "schedule", "action"},
		},
		Source: "bundled",
	})
	RegisterBuiltin("cron_create", cronCreateHandler)

	r.Register(&ToolDef{
		Name:        "cron_list",
		Description: "List all scheduled tasks for the current agent.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Source: "bundled",
	})
	RegisterBuiltin("cron_list", cronListHandler)

	r.Register(&ToolDef{
		Name:        "cron_delete",
		Description: "Delete a scheduled task by its ID.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cron_id": map[string]any{
					"type":        "string",
					"description": "The cron job ID to delete",
				},
			},
			"required": []string{"cron_id"},
		},
		Source: "bundled",
	})
	RegisterBuiltin("cron_delete", cronDeleteHandler)
}

func cronCreateHandler(ctx context.Context, input map[string]any) (string, error) {
	if cronCreateFn == nil {
		return "", fmt.Errorf("cron scheduling not available")
	}
	agentID, _ := input["_agent_id"].(string)
	name, _ := input["name"].(string)
	schedule, _ := input["schedule"].(string)
	action, _ := input["action"].(string)
	if name == "" || schedule == "" || action == "" {
		return "", fmt.Errorf("name, schedule, and action are required")
	}
	return cronCreateFn(ctx, agentID, name, schedule, action)
}

func cronListHandler(ctx context.Context, input map[string]any) (string, error) {
	if cronListFn == nil {
		return "", fmt.Errorf("cron scheduling not available")
	}
	agentID, _ := input["_agent_id"].(string)
	results, err := cronListFn(ctx, agentID)
	if err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	return string(data), nil
}

func cronDeleteHandler(ctx context.Context, input map[string]any) (string, error) {
	if cronDeleteFn == nil {
		return "", fmt.Errorf("cron scheduling not available")
	}
	agentID, _ := input["_agent_id"].(string)
	cronID, _ := input["cron_id"].(string)
	if cronID == "" {
		return "", fmt.Errorf("cron_id is required")
	}
	if err := cronDeleteFn(ctx, agentID, cronID); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted cron job %s", cronID), nil
}

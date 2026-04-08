package nibble

import (
	"context"
	"fmt"
)

// MessageSendFunc sends a message to a channel (Telegram, Discord, Slack, etc.).
type MessageSendFunc func(ctx context.Context, channel, target, content string) (string, error)

var messageSendFn MessageSendFunc

// SetMessageSendFunc sets the cross-channel message sending function.
func SetMessageSendFunc(fn MessageSendFunc) {
	messageSendFn = fn
}

// registerMessageTools registers cross-channel messaging tools.
// group:messaging — message
func registerMessageTools(r *Registry) {
	r.Register(&ToolDef{
		Name:        "message",
		Description: "Send a message through a specific channel (Telegram, Discord, Slack, webhook, etc.). Use this when the agent needs to proactively communicate outside the current session.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel": map[string]any{
					"type":        "string",
					"description": "The channel type: telegram, discord, slack, webhook",
					"enum":        []string{"telegram", "discord", "slack", "webhook"},
				},
				"target": map[string]any{
					"type":        "string",
					"description": "Target identifier (chat ID, channel ID, webhook URL, etc.)",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The message content to send",
				},
			},
			"required": []string{"channel", "target", "content"},
		},
		Source: "bundled",
	})
	RegisterBuiltin("message", messageHandler)
}

func messageHandler(ctx context.Context, input map[string]any) (string, error) {
	if messageSendFn == nil {
		return "", fmt.Errorf("messaging not configured")
	}

	channel, _ := input["channel"].(string)
	target, _ := input["target"].(string)
	content, _ := input["content"].(string)
	if channel == "" || target == "" || content == "" {
		return "", fmt.Errorf("channel, target, and content are required")
	}

	return messageSendFn(ctx, channel, target, content)
}

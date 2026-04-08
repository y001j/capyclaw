package instinct

// Default prompt templates.
const (
	DefaultSystemPromptTemplate = `You are {{.AgentName}}, an AI assistant powered by CapyClaw.

{{if .IdentityMD}}{{.IdentityMD}}{{end}}

## Current Context
- Date: {{.CurrentDate}}
- Session: {{.SessionKey}}
- User: {{.UserName}}

{{if .ContextSummary}}## Previous Context Summary
{{.ContextSummary}}{{end}}
`

	CompactionPromptTemplate = `Summarize the following conversation, preserving:
1. All entity names, dates, and specific facts mentioned
2. Key decisions made and their rationale
3. Any pending tasks or action items
4. Important context needed for future messages

Conversation:
{{.Messages}}

Provide a concise summary that captures all critical information.`
)

package instinct

import (
	"context"
	"strings"
)

// PromptBuilder assembles system prompts from identity, skills, and memory components.
type PromptBuilder struct{}

// Build assembles the full system prompt for an agent.
func (b *PromptBuilder) Build(ctx context.Context, req *BuildRequest) (string, error) {
	var parts []string

	if req.IdentityMD != "" {
		parts = append(parts, req.IdentityMD)
	}

	if req.SoulMD != "" {
		parts = append(parts, req.SoulMD)
	}

	if req.UserMD != "" {
		parts = append(parts, req.UserMD)
	}

	if len(req.Skills) > 0 {
		parts = append(parts, buildSkillsXML(req.Skills))
	}

	if req.MemoryContext != "" {
		parts = append(parts, "## Relevant Memories\n"+req.MemoryContext)
	}

	if req.ContextSummary != "" {
		parts = append(parts, "## Context Summary\n"+req.ContextSummary)
	}

	return strings.Join(parts, "\n\n---\n\n"), nil
}

// BuildRequest contains all inputs for prompt assembly.
type BuildRequest struct {
	IdentityMD     string
	SoulMD         string
	UserMD         string
	Skills         []SkillPrompt
	ContextSummary string
	MemoryContext  string
}

// SkillPrompt contains a skill's prompt contribution.
type SkillPrompt struct {
	Name        string
	Description string
	Content     string
}

func buildSkillsXML(skills []SkillPrompt) string {
	var sb strings.Builder
	sb.WriteString("<skills>\n")
	for _, s := range skills {
		sb.WriteString("  <skill name=\"")
		sb.WriteString(s.Name)
		sb.WriteString("\">\n")
		sb.WriteString("    ")
		sb.WriteString(s.Content)
		sb.WriteString("\n  </skill>\n")
	}
	sb.WriteString("</skills>")
	return sb.String()
}

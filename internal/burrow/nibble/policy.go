package nibble

// PolicyAction defines what happens when a tool is invoked.
type PolicyAction string

const (
	PolicyAllow PolicyAction = "allow"
	PolicyDeny  PolicyAction = "deny"
	PolicyAsk   PolicyAction = "ask"
)

// ToolPolicy defines cascading tool execution policies.
// Policies cascade: global → tenant → agent → channel.
type ToolPolicy struct {
	DefaultAction PolicyAction            `json:"default"`
	ToolOverrides map[string]PolicyAction `json:"tool_overrides,omitempty"`
}

// Evaluate determines the action for a given tool based on cascading policies.
func EvaluatePolicy(tool string, policies ...ToolPolicy) PolicyAction {
	// Apply policies in order (later policies override earlier ones)
	action := PolicyAsk // default

	for _, p := range policies {
		if p.DefaultAction != "" {
			action = p.DefaultAction
		}
		if override, ok := p.ToolOverrides[tool]; ok {
			action = override
		}
	}

	return action
}

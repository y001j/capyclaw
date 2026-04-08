package nibble

// ToolGroups maps group names to the tools they contain.
// Used by skill manifests and policies to reference tools by group.
var ToolGroups = map[string][]string{
	"runtime":    {"exec", "bash"},
	"fs":         {"read", "write", "edit", "list_files"},
	"web":        {"web_search", "fetch_url"},
	"memory":     {"memory_search"},
	"sessions":   {"sessions_list", "sessions_history", "sessions_send"},
	"messaging":  {"message"},
	"automation": {"cron_create", "cron_list", "cron_delete"},
	"util":       {"current_time", "text_transform", "json_tool", "calculator"},
}

// RegisterBuiltinTools registers all built-in tool definitions into the given registry.
// This must be called after the registry is created, as it registers both the ToolDef
// (so the LLM knows about the tool) and the BuiltinHandler (so the executor can run it).
func RegisterBuiltinTools(r *Registry) {
	registerExecTools(r)
	registerFSTools(r)
	registerWebTools(r)
	registerUtilTools(r)
	registerMemoryTools(r)
	registerSessionTools(r)
	registerMessageTools(r)
	registerCronTools(r)
}

// ExpandGroups expands group:xxx references into individual tool names.
// Input like ["group:fs", "exec", "group:web"] returns ["read", "write", "edit", "list_files", "exec", "web_search", "fetch_url"].
func ExpandGroups(names []string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, name := range names {
		if len(name) > 6 && name[:6] == "group:" {
			groupName := name[6:]
			if groupName == "all" {
				for _, tools := range ToolGroups {
					for _, t := range tools {
						if !seen[t] {
							result = append(result, t)
							seen[t] = true
						}
					}
				}
			} else if tools, ok := ToolGroups[groupName]; ok {
				for _, t := range tools {
					if !seen[t] {
						result = append(result, t)
						seen[t] = true
					}
				}
			}
		} else {
			if !seen[name] {
				result = append(result, name)
				seen[name] = true
			}
		}
	}
	return result
}

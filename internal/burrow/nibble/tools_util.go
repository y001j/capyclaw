package nibble

// registerUtilTools registers utility tools into the registry.
// group:util — current_time, text_transform, json_tool, calculator
// Note: handler implementations remain in builtins.go init().
// This file only registers the ToolDef for LLM visibility.
func registerUtilTools(r *Registry) {
	r.Register(&ToolDef{
		Name:        "current_time",
		Description: "Get the current date and time, optionally in a specific timezone.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"timezone": map[string]any{
					"type":        "string",
					"description": "IANA timezone name (e.g., 'Asia/Shanghai', 'America/New_York'). Default: server local time.",
				},
			},
		},
		Source: "bundled",
	})

	r.Register(&ToolDef{
		Name:        "text_transform",
		Description: "Perform text transformations: word_count, uppercase, lowercase, reverse, trim.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "The text to transform",
				},
				"operation": map[string]any{
					"type":        "string",
					"description": "The operation to perform",
					"enum":        []string{"word_count", "uppercase", "lowercase", "reverse", "trim"},
				},
			},
			"required": []string{"text", "operation"},
		},
		Source: "bundled",
	})

	r.Register(&ToolDef{
		Name:        "json_tool",
		Description: "Parse, format, minify, or validate JSON data.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"data": map[string]any{
					"type":        "string",
					"description": "The JSON string to process",
				},
				"operation": map[string]any{
					"type":        "string",
					"description": "The operation to perform",
					"enum":        []string{"format", "minify", "validate"},
				},
			},
			"required": []string{"data"},
		},
		Source: "bundled",
	})

	r.Register(&ToolDef{
		Name:        "calculator",
		Description: "Evaluate a mathematical expression.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{
					"type":        "string",
					"description": "The mathematical expression to evaluate",
				},
			},
			"required": []string{"expression"},
		},
		Source: "bundled",
	})
}

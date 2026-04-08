package nibble

// registerWebTools registers web search and fetch tools into the registry.
// group:web — web_search, fetch_url
// Note: search handler implementations remain in builtins.go (bochaSearch, tavilySearch, etc.)
// This file only registers the ToolDef for LLM visibility.
func registerWebTools(r *Registry) {
	r.Register(&ToolDef{
		Name:        "web_search",
		Description: "Search the web for information. Returns a list of relevant results with titles, snippets, and source URLs.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query",
				},
			},
			"required": []string{"query"},
		},
		Source: "bundled",
	})

	r.Register(&ToolDef{
		Name:        "fetch_url",
		Description: "Fetch the content of a URL. Returns the page content as text (HTML tags stripped) or formatted JSON.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch",
				},
			},
			"required": []string{"url"},
		},
		Source: "bundled",
	})
}

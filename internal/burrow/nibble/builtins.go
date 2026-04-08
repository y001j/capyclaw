package nibble

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"CapyClaw/internal/shared/config"
)

// BuiltinHandler executes a built-in tool directly in-process.
type BuiltinHandler func(ctx context.Context, input map[string]any) (string, error)

// builtinHandlers maps tool names to their in-process handlers.
var builtinHandlers = map[string]BuiltinHandler{}

// RegisterBuiltin adds a built-in tool handler.
func RegisterBuiltin(name string, handler BuiltinHandler) {
	builtinHandlers[name] = handler
}

// GetBuiltin returns a built-in handler if one exists for the given tool name.
func GetBuiltin(name string) (BuiltinHandler, bool) {
	h, ok := builtinHandlers[name]
	return h, ok
}

// RegisterSearchTools registers search tool handlers based on configured API keys.
// Called at server startup with the loaded config.
func RegisterSearchTools(cfg *config.SearchProvidersConfig) {
	if cfg == nil {
		// No search config — register DuckDuckGo as fallback
		RegisterBuiltin("web_search", ddgSearch)
		return
	}

	// Bocha (博查)
	if cfg.Bocha.APIKey != "" {
		apiKey := cfg.Bocha.APIKey
		baseURL := cfg.Bocha.BaseURL
		if baseURL == "" {
			baseURL = "https://api.bocha.cn/v1"
		}
		RegisterBuiltin("web_search_bocha", func(ctx context.Context, input map[string]any) (string, error) {
			return bochaSearch(ctx, apiKey, baseURL, input)
		})
	}

	// Tavily
	if cfg.Tavily.APIKey != "" {
		apiKey := cfg.Tavily.APIKey
		baseURL := cfg.Tavily.BaseURL
		if baseURL == "" {
			baseURL = "https://api.tavily.com"
		}
		RegisterBuiltin("web_search_tavily", func(ctx context.Context, input map[string]any) (string, error) {
			return tavilySearch(ctx, apiKey, baseURL, input)
		})
	}

	// Brave
	if cfg.Brave.APIKey != "" {
		apiKey := cfg.Brave.APIKey
		baseURL := cfg.Brave.BaseURL
		if baseURL == "" {
			baseURL = "https://api.search.brave.com/res/v1"
		}
		RegisterBuiltin("web_search_brave", func(ctx context.Context, input map[string]any) (string, error) {
			return braveSearch(ctx, apiKey, baseURL, input)
		})
	}

	// Always register DuckDuckGo as the generic fallback
	RegisterBuiltin("web_search", ddgSearch)
}

// ========== Search Implementations ==========

// --- Bocha (博查) Search ---
func bochaSearch(ctx context.Context, apiKey, baseURL string, input map[string]any) (string, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	freshness, _ := input["freshness"].(string)
	if freshness == "" {
		freshness = "noLimit"
	}
	summary := true
	if v, ok := input["summary"].(bool); ok {
		summary = v
	}
	count := 10
	if v, ok := input["count"].(float64); ok && v > 0 {
		count = int(v)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"query":     query,
		"freshness": freshness,
		"summary":   summary,
		"count":     count,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/web-search", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("bocha request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100000))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bocha API error (%d): %s", resp.StatusCode, string(body))
	}

	// Parse Bocha response
	var result struct {
		Data struct {
			Webpages struct {
				Results []struct {
					Name    string `json:"name"`
					URL     string `json:"url"`
					Summary string `json:"summary"`
				} `json:"value"`
			} `json:"webPages"`
			Summary string `json:"summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing bocha response: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s (via Bocha)\n\n", query))

	if result.Data.Summary != "" {
		sb.WriteString("**Summary**: " + result.Data.Summary + "\n\n")
	}

	for i, r := range result.Data.Webpages.Results {
		if i >= 8 {
			break
		}
		snippet := r.Summary
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   Source: %s\n\n", i+1, r.Name, snippet, r.URL))
	}
	return sb.String(), nil
}

// --- Tavily Search ---
func tavilySearch(ctx context.Context, apiKey, baseURL string, input map[string]any) (string, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	reqBody, _ := json.Marshal(map[string]any{
		"query":              query,
		"search_depth":       "basic",
		"include_answer":     true,
		"max_results":        8,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/search", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tavily request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100000))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tavily API error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Answer  string `json:"answer"`
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing tavily response: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s (via Tavily)\n\n", query))

	if result.Answer != "" {
		sb.WriteString("**Answer**: " + result.Answer + "\n\n")
	}

	for i, r := range result.Results {
		if i >= 8 {
			break
		}
		snippet := r.Content
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   Source: %s\n\n", i+1, r.Title, snippet, r.URL))
	}
	return sb.String(), nil
}

// --- Brave Search ---
func braveSearch(ctx context.Context, apiKey, baseURL string, input map[string]any) (string, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	reqURL := baseURL + "/web/search?q=" + url.QueryEscape(query) + "&count=8"
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("brave request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100000))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("brave API error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing brave response: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s (via Brave)\n\n", query))

	for i, r := range result.Web.Results {
		if i >= 8 {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   Source: %s\n\n", i+1, r.Title, r.Description, r.URL))
	}
	return sb.String(), nil
}

// --- DuckDuckGo (free fallback, no API key needed) ---
func ddgSearch(ctx context.Context, input map[string]any) (string, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "CapyClaw/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50000))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	results := extractDDGResults(string(body))
	if len(results) == 0 {
		return fmt.Sprintf("No results found for: %s", query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s (via DuckDuckGo)\n\n", query))
	for i, r := range results {
		if i >= 8 {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n\n", i+1, r.title, r.snippet))
	}
	return sb.String(), nil
}

// ========== Utility Tools ==========

func init() {
	// --- fetch_url: fetch content from a URL ---
	RegisterBuiltin("fetch_url", func(ctx context.Context, input map[string]any) (string, error) {
		rawURL, _ := input["url"].(string)
		if rawURL == "" {
			return "", fmt.Errorf("url is required")
		}

		req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "CapyClaw/1.0")

		resp, err := httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("fetch failed: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(io.LimitReader(resp.Body, 100000))
		if err != nil {
			return "", fmt.Errorf("reading response: %w", err)
		}

		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(contentType, "application/json") {
			var parsed any
			if json.Unmarshal(body, &parsed) == nil {
				pretty, _ := json.MarshalIndent(parsed, "", "  ")
				return string(pretty), nil
			}
		}
		if strings.Contains(contentType, "text/html") {
			return stripHTMLTags(string(body)), nil
		}
		return string(body), nil
	})

	// --- current_time: get current date and time ---
	RegisterBuiltin("current_time", func(ctx context.Context, input map[string]any) (string, error) {
		tz, _ := input["timezone"].(string)
		loc := time.Now().Location()
		if tz != "" {
			if l, err := time.LoadLocation(tz); err == nil {
				loc = l
			}
		}
		now := time.Now().In(loc)
		return fmt.Sprintf("Current time: %s (%s)", now.Format("2006-01-02 15:04:05 MST"), loc.String()), nil
	})

	// --- text_transform: text processing utilities ---
	RegisterBuiltin("text_transform", func(ctx context.Context, input map[string]any) (string, error) {
		text, _ := input["text"].(string)
		op, _ := input["operation"].(string)
		if text == "" || op == "" {
			return "", fmt.Errorf("text and operation are required")
		}

		switch op {
		case "word_count":
			words := strings.Fields(text)
			chars := len([]rune(text))
			lines := strings.Count(text, "\n") + 1
			return fmt.Sprintf("Words: %d, Characters: %d, Lines: %d", len(words), chars, lines), nil
		case "uppercase":
			return strings.ToUpper(text), nil
		case "lowercase":
			return strings.ToLower(text), nil
		case "reverse":
			runes := []rune(text)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return string(runes), nil
		case "trim":
			return strings.TrimSpace(text), nil
		default:
			return "", fmt.Errorf("unknown operation: %s (supported: word_count, uppercase, lowercase, reverse, trim)", op)
		}
	})

	// --- json_tool: parse, format, and query JSON ---
	RegisterBuiltin("json_tool", func(ctx context.Context, input map[string]any) (string, error) {
		data, _ := input["data"].(string)
		op, _ := input["operation"].(string)
		if data == "" {
			return "", fmt.Errorf("data is required")
		}
		if op == "" {
			op = "format"
		}

		switch op {
		case "format":
			var parsed any
			if err := json.Unmarshal([]byte(data), &parsed); err != nil {
				return "", fmt.Errorf("invalid JSON: %w", err)
			}
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			return string(pretty), nil
		case "minify":
			var parsed any
			if err := json.Unmarshal([]byte(data), &parsed); err != nil {
				return "", fmt.Errorf("invalid JSON: %w", err)
			}
			compact, _ := json.Marshal(parsed)
			return string(compact), nil
		case "validate":
			var parsed any
			if err := json.Unmarshal([]byte(data), &parsed); err != nil {
				return fmt.Sprintf("Invalid JSON: %s", err.Error()), nil
			}
			return "Valid JSON", nil
		default:
			return "", fmt.Errorf("unknown operation: %s (supported: format, minify, validate)", op)
		}
	})

	// --- calculator ---
	RegisterBuiltin("calculator", func(ctx context.Context, input map[string]any) (string, error) {
		expr, _ := input["expression"].(string)
		if expr == "" {
			return "", fmt.Errorf("expression is required")
		}
		return fmt.Sprintf("Expression: %s (please compute this manually — safe eval not yet implemented)", expr), nil
	})
}

// ========== Helpers ==========

var httpClient = &http.Client{Timeout: 15 * time.Second}

type searchResult struct {
	title   string
	snippet string
}

func extractDDGResults(html string) []searchResult {
	var results []searchResult
	parts := strings.Split(html, "result__a")
	for i := 1; i < len(parts) && len(results) < 10; i++ {
		part := parts[i]
		title := extractBetween(part, ">", "</a>")
		title = stripHTMLTags(title)
		title = strings.TrimSpace(title)

		snippet := ""
		if idx := strings.Index(part, "result__snippet"); idx >= 0 {
			snippet = extractBetween(part[idx:], ">", "</")
			snippet = stripHTMLTags(snippet)
			snippet = strings.TrimSpace(snippet)
		}

		if title != "" {
			results = append(results, searchResult{title: title, snippet: snippet})
		}
	}
	return results
}

func extractBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return s
	}
	return s[:j]
}

func stripHTMLTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return strings.TrimSpace(result.String())
}

package nibble

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// registerFSTools registers file system tools.
// group:fs — read, write, edit, list_files
func registerFSTools(r *Registry) {
	// read: read file content
	r.Register(&ToolDef{
		Name:        "read",
		Description: "Read the contents of a file. Supports reading specific line ranges for large files.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path (relative to workspace or absolute)",
				},
				"start_line": map[string]any{
					"type":        "integer",
					"description": "Start reading from this line number (1-based, default: 1)",
				},
				"end_line": map[string]any{
					"type":        "integer",
					"description": "Stop reading at this line number (inclusive, default: end of file)",
				},
			},
			"required": []string{"path"},
		},
		Source: "bundled",
	})
	RegisterBuiltin("read", readHandler)

	// write: write or create a file
	r.Register(&ToolDef{
		Name:        "write",
		Description: "Write content to a file, creating it if it doesn't exist. Overwrites existing content. Creates parent directories automatically.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path to write to",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The content to write",
				},
			},
			"required": []string{"path", "content"},
		},
		Source: "bundled",
	})
	RegisterBuiltin("write", writeHandler)

	// edit: apply targeted edits to a file
	r.Register(&ToolDef{
		Name:        "edit",
		Description: "Apply a targeted edit to a file by replacing a specific text segment. More precise than rewriting the entire file.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path to edit",
				},
				"old_text": map[string]any{
					"type":        "string",
					"description": "The exact text to find and replace (must match exactly)",
				},
				"new_text": map[string]any{
					"type":        "string",
					"description": "The replacement text",
				},
			},
			"required": []string{"path", "old_text", "new_text"},
		},
		Source: "bundled",
	})
	RegisterBuiltin("edit", editHandler)

	// list_files: list directory contents
	r.Register(&ToolDef{
		Name:        "list_files",
		Description: "List files and directories at a given path. Supports recursive listing with glob patterns.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Directory path (default: current workspace)",
				},
				"pattern": map[string]any{
					"type":        "string",
					"description": "Glob pattern to filter results (e.g., '*.go', '**/*.yaml')",
				},
				"recursive": map[string]any{
					"type":        "boolean",
					"description": "List recursively (default: false)",
				},
			},
		},
		Source: "bundled",
	})
	RegisterBuiltin("list_files", listFilesHandler)
}

func readHandler(ctx context.Context, input map[string]any) (string, error) {
	path, _ := input["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	startLine := 1
	endLine := len(lines)
	if v, ok := input["start_line"].(float64); ok && v > 0 {
		startLine = int(v)
	}
	if v, ok := input["end_line"].(float64); ok && v > 0 {
		endLine = int(v)
	}

	if startLine > len(lines) {
		return "", fmt.Errorf("start_line %d exceeds file length %d", startLine, len(lines))
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine < 1 {
		startLine = 1
	}

	var sb strings.Builder
	for i := startLine - 1; i < endLine; i++ {
		sb.WriteString(fmt.Sprintf("%4d | %s\n", i+1, lines[i]))
	}

	// Truncate if too large
	result := sb.String()
	if len(result) > 100000 {
		result = result[:100000] + "\n... [output truncated, use start_line/end_line to read specific sections]"
	}

	return result, nil
}

func writeHandler(ctx context.Context, input map[string]any) (string, error) {
	path, _ := input["path"].(string)
	content, _ := input["content"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	// Prevent writing outside workspace (basic check)
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path traversal not allowed")
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}

	lines := strings.Count(content, "\n") + 1
	return fmt.Sprintf("Wrote %d bytes (%d lines) to %s", len(content), lines, path), nil
}

func editHandler(ctx context.Context, input map[string]any) (string, error) {
	path, _ := input["path"].(string)
	oldText, _ := input["old_text"].(string)
	newText, _ := input["new_text"].(string)
	if path == "" || oldText == "" {
		return "", fmt.Errorf("path and old_text are required")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return "", fmt.Errorf("old_text not found in file")
	}
	if count > 1 {
		return "", fmt.Errorf("old_text matches %d locations, please provide more context to make it unique", count)
	}

	newContent := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}

	return fmt.Sprintf("Applied edit to %s: replaced %d chars with %d chars", path, len(oldText), len(newText)), nil
}

func listFilesHandler(ctx context.Context, input map[string]any) (string, error) {
	path, _ := input["path"].(string)
	if path == "" {
		path = "."
	}
	pattern, _ := input["pattern"].(string)
	recursive, _ := input["recursive"].(bool)

	var files []string

	if pattern != "" && recursive {
		// Glob pattern search
		err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			matched, _ := filepath.Match(pattern, filepath.Base(p))
			if matched {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walking directory: %w", err)
		}
	} else if recursive {
		err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(path, p)
			if info.IsDir() {
				files = append(files, rel+"/")
			} else {
				files = append(files, rel)
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walking directory: %w", err)
		}
	} else {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", fmt.Errorf("reading directory: %w", err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if pattern != "" {
				matched, _ := filepath.Match(pattern, name)
				if !matched {
					continue
				}
			}
			if entry.IsDir() {
				name += "/"
			}
			info, err := entry.Info()
			if err == nil {
				files = append(files, fmt.Sprintf("%s  (%d bytes)", name, info.Size()))
			} else {
				files = append(files, name)
			}
		}
	}

	if len(files) == 0 {
		return "No files found", nil
	}
	if len(files) > 500 {
		files = files[:500]
		files = append(files, fmt.Sprintf("... and more (showing first 500 of %d)", len(files)))
	}

	return strings.Join(files, "\n"), nil
}

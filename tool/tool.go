// Package tool defines model-visible Tool definitions and executes built-in tools.
package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hsrvms/deku/provider"
)

// Tool is a model-invokable built-in capability.
type Tool interface {
	Definition() provider.ToolDefinition
	Execute(context.Context, json.RawMessage) (string, error)
}

// Registry owns the tools available to one Agent and confines them to a
// repository root.
type Registry struct {
	root  string
	tools map[string]Tool
}

// NewRegistry creates the read-only filesystem tool registry rooted at root.
// Tool paths must be relative to root and cannot escape it.
func NewRegistry(root string) (*Registry, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("tool repository root is required")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve tool repository root: %w", err)
	}
	absoluteRoot, err = filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve tool repository root: %w", err)
	}
	info, err := os.Stat(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("stat tool repository root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("tool repository root %q is not a directory", root)
	}

	filesystem := &filesystem{root: absoluteRoot}
	tools := []Tool{
		&grepTool{filesystem: filesystem},
		&lsTool{filesystem: filesystem},
		&readTool{filesystem: filesystem},
	}
	registry := &Registry{
		root:  absoluteRoot,
		tools: make(map[string]Tool, len(tools)),
	}
	for _, tool := range tools {
		name := tool.Definition().Function.Name
		registry.tools[name] = tool
	}
	return registry, nil
}

// Definitions returns the model-visible definitions in stable name order.
func (r *Registry) Definitions() []provider.ToolDefinition {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	definitions := make([]provider.ToolDefinition, 0, len(names))
	for _, name := range names {
		definitions = append(definitions, r.tools[name].Definition())
	}
	return definitions
}

// Execute validates and runs a named tool. Tool failures are returned to the
// Agent so it can normalize them into a Tool Result for the model.
func (r *Registry) Execute(ctx context.Context, name string, arguments string) (string, error) {
	if r == nil {
		return "", errors.New("tool registry is nil")
	}
	if ctx == nil {
		return "", errors.New("tool context is nil")
	}
	tool, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return tool.Execute(ctx, json.RawMessage(arguments))
}

type filesystem struct {
	root string
}

func (f *filesystem) resolve(ctx context.Context, path string, allowRoot bool) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		if allowRoot {
			return f.root, nil
		}
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(path) {
		return "", errors.New("path must be relative to the repository root")
	}
	cleaned := filepath.Clean(path)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("path must remain within the repository root")
	}
	candidate := filepath.Join(f.root, cleaned)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	inside, err := filepath.Rel(f.root, resolved)
	if err != nil {
		return "", fmt.Errorf("check path %q: %w", path, err)
	}
	if inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return "", errors.New("path must remain within the repository root")
	}
	return resolved, nil
}

type lsTool struct{ filesystem *filesystem }

type lsArguments struct {
	Path string `json:"path"`
}

func (t *lsTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "ls",
			Description: "List the entries in a repository directory.",
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Repository-relative directory path; omit it for the repository root.",
				},
			}),
		},
	}
}

func (t *lsTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var arguments lsArguments
	if err := decodeArguments(raw, &arguments); err != nil {
		return "", fmt.Errorf("ls arguments: %w", err)
	}
	path, err := t.filesystem.resolve(ctx, arguments.Path, true)
	if err != nil {
		return "", fmt.Errorf("ls path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("ls %q: %w", arguments.Path, err)
	}
	if !info.IsDir() {
		return filepath.Base(path) + "\n", nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("list %q: %w", arguments.Path, err)
	}
	var result strings.Builder
	for _, entry := range entries {
		if err := contextError(ctx); err != nil {
			return "", err
		}
		result.WriteString(entry.Name())
		if entry.IsDir() {
			result.WriteByte('/')
		}
		result.WriteByte('\n')
	}
	return result.String(), nil
}

type readTool struct{ filesystem *filesystem }

type readArguments struct {
	Path      string `json:"path"`
	StartLine *int   `json:"start_line"`
	EndLine   *int   `json:"end_line"`
}

func (t *readTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "read",
			Description: "Read a repository file, optionally limited to an inclusive line range.",
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Repository-relative file path.",
				},
				"start_line": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"description": "First 1-based line to return.",
				},
				"end_line": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"description": "Last 1-based line to return, inclusive.",
				},
			}, "path"),
		},
	}
}

func (t *readTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var arguments readArguments
	if err := decodeArguments(raw, &arguments); err != nil {
		return "", fmt.Errorf("read arguments: %w", err)
	}
	path, err := t.filesystem.resolve(ctx, arguments.Path, false)
	if err != nil {
		return "", fmt.Errorf("read path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", arguments.Path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("read %q: path is a directory", arguments.Path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", arguments.Path, err)
	}
	if arguments.StartLine == nil && arguments.EndLine == nil {
		return string(data), nil
	}
	start, end, err := lineRange(arguments.StartLine, arguments.EndLine, data)
	if err != nil {
		return "", err
	}
	lines := splitLines(data)
	return strings.Join(lines[start-1:end], ""), nil
}

type grepTool struct{ filesystem *filesystem }

type grepArguments struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func (t *grepTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "grep",
			Description: "Search repository text with a regular expression.",
			Parameters: objectSchema(map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Go regular expression to search for.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional repository-relative file or directory filter.",
				},
			}, "pattern"),
		},
	}
}

func (t *grepTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var arguments grepArguments
	if err := decodeArguments(raw, &arguments); err != nil {
		return "", fmt.Errorf("grep arguments: %w", err)
	}
	if strings.TrimSpace(arguments.Pattern) == "" {
		return "", errors.New("grep pattern is required")
	}
	pattern, err := regexp.Compile(arguments.Pattern)
	if err != nil {
		return "", fmt.Errorf("grep pattern: %w", err)
	}
	path, err := t.filesystem.resolve(ctx, arguments.Path, true)
	if err != nil {
		return "", fmt.Errorf("grep path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("grep %q: %w", arguments.Path, err)
	}
	var result strings.Builder
	if info.IsDir() {
		err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := contextError(ctx); err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			return appendMatches(&result, t.filesystem.root, current, pattern, ctx)
		})
	} else {
		err = appendMatches(&result, t.filesystem.root, path, pattern, ctx)
	}
	if err != nil {
		return "", fmt.Errorf("grep: %w", err)
	}
	return result.String(), nil
}

func appendMatches(result *strings.Builder, root, path string, pattern *regexp.Regexp, ctx context.Context) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	relative = filepath.ToSlash(relative)
	for lineNumber, line := range splitLines(data) {
		if err := contextError(ctx); err != nil {
			return err
		}
		text := strings.TrimSuffix(line, "\n")
		text = strings.TrimSuffix(text, "\r")
		if pattern.MatchString(text) {
			fmt.Fprintf(result, "%s:%d:%s\n", relative, lineNumber+1, text)
		}
	}
	return nil
}

func decodeArguments(raw json.RawMessage, target any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("arguments must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("arguments contain trailing JSON")
		}
		return err
	}
	return nil
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func lineRange(startLine, endLine *int, data []byte) (int, int, error) {
	lines := splitLines(data)
	start := 1
	if startLine != nil {
		start = *startLine
	}
	end := len(lines)
	if endLine != nil {
		end = *endLine
	}
	if start < 1 || end < 1 {
		return 0, 0, errors.New("line numbers must be positive")
	}
	if start > end {
		return 0, 0, errors.New("start_line must not exceed end_line")
	}
	if start > len(lines) {
		return 0, 0, fmt.Errorf("start_line %d exceeds file length", start)
	}
	if end > len(lines) {
		end = len(lines)
	}
	return start, end, nil
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	parts := bytes.SplitAfter(data, []byte{'\n'})
	lines := make([]string, len(parts))
	for index, part := range parts {
		lines[index] = string(part)
	}
	return lines
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("tool context is nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

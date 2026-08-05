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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/edit"
	"github.com/hsrvms/deku/provider"
)

// Tool is a model-invokable built-in capability. Tier declares the tool's
// classification so the Approval gate can decide whether it runs unprompted.
type Tool interface {
	Definition() provider.ToolDefinition
	Execute(context.Context, json.RawMessage) (string, error)
	Tier() approval.Tier
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
		&commandTool{filesystem: filesystem},
		&editTool{filesystem: filesystem},
		&grepTool{filesystem: filesystem},
		&lsTool{filesystem: filesystem},
		&readTool{filesystem: filesystem},
		&writeTool{filesystem: filesystem},
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

// Root returns the absolute repository root the Registry confines tools to.
func (r *Registry) Root() string {
	if r == nil {
		return ""
	}
	return r.root
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

// Tier returns the classification the named tool declares. The Agent uses it
// to decide whether Approval is required before execution.
func (r *Registry) Tier(name string) (approval.Tier, error) {
	if r == nil {
		return "", errors.New("tool registry is nil")
	}
	tool, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return tool.Tier(), nil
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
	if err := f.confineToRoot(resolved); err != nil {
		return "", fmt.Errorf("check path %q: %w", path, err)
	}
	return resolved, nil
}

// confineToRoot reports an error when resolved escapes the repository root.
func (f *filesystem) confineToRoot(resolved string) error {
	inside, err := filepath.Rel(f.root, resolved)
	if err != nil {
		return fmt.Errorf("compare with repository root: %w", err)
	}
	if inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return errors.New("path must remain within the repository root")
	}
	return nil
}

// resolveCreatePath confines a not-yet-existing target to the repository root.
// Unlike resolve, the target itself may be absent; the deepest existing
// ancestor is resolved and must stay inside the root, so writing through a
// symlink that escapes the root is rejected while a missing leaf is allowed.
func (f *filesystem) resolveCreatePath(ctx context.Context, path string) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
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
	resolved, err := resolveExistingAncestor(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	if err := f.confineToRoot(resolved); err != nil {
		return "", fmt.Errorf("check path %q: %w", path, err)
	}
	return candidate, nil
}

// resolveExistingAncestor returns the symlink-resolved path of the deepest
// existing ancestor of path. It walks up until it finds a path that exists, so
// a not-yet-created target's parent directory can still be rooted and checked
// for confinement.
func resolveExistingAncestor(path string) (string, error) {
	current := path
	for {
		_, err := os.Stat(current)
		if err == nil {
			resolved, evalErr := filepath.EvalSymlinks(current)
			if evalErr != nil {
				return "", evalErr
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("no existing ancestor to resolve")
		}
		current = parent
	}
}

// commandDefaultTimeout is the fallback deadline for a command call when the
// caller supplies no explicit timeout. It bounds runaway processes so a single
// Tool Call cannot hang the Agent indefinitely.
const commandDefaultTimeout = 120 * time.Second

type commandTool struct {
	filesystem *filesystem
}

type commandArguments struct {
	Command string `json:"command"`
	Dir     string `json:"dir"`
	Timeout int    `json:"timeout"`
}

func (t *commandTool) Tier() approval.Tier { return approval.Destructive }

func (t *commandTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "command",
			Description: "Run a shell command in the repository, capturing stdout, stderr, and the exit code. The command is destructive and always requires Approval; set dir to a repository-relative working directory and timeout to a deadline in whole seconds (default 120s).",
			Parameters: objectSchema(map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to run.",
				},
				"dir": map[string]any{
					"type":        "string",
					"description": "Optional repository-relative working directory; defaults to the repository root.",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"description": "Optional deadline in whole seconds; the command is killed when it exceeds this.",
				},
			}, "command"),
		},
	}
}

func (t *commandTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var arguments commandArguments
	if err := decodeArguments(raw, &arguments); err != nil {
		return "", fmt.Errorf("command arguments: %w", err)
	}
	if strings.TrimSpace(arguments.Command) == "" {
		return "", errors.New("command is required")
	}
	if arguments.Timeout < 0 {
		return "", errors.New("command timeout must be positive")
	}
	dir, err := t.filesystem.resolve(ctx, arguments.Dir, true)
	if err != nil {
		return "", fmt.Errorf("command directory: %w", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("command directory %q: %w", arguments.Dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("command directory %q is not a directory", arguments.Dir)
	}
	timeout := commandDefaultTimeout
	if arguments.Timeout > 0 {
		timeout = time.Duration(arguments.Timeout) * time.Second
	}
	return runCommand(ctx, dir, arguments.Command, timeout)
}

// runCommand executes command through the shell in dir, capturing stdout and
// stderr and reporting the exit code. A deadline or a canceled caller context
// kills the process; a timeout is reported as an error rather than a fake exit
// code so the Agent knows the command never completed.
func runCommand(ctx context.Context, dir, command string, timeout time.Duration) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitCode int
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("execute command: %w", ctx.Err())
		}
		if cmdCtx.Err() != nil {
			return "", fmt.Errorf("command timed out after %s: %w", timeout, cmdCtx.Err())
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return "", fmt.Errorf("execute command: %w", err)
		}
		exitCode = exitErr.ExitCode()
	}
	return formatCommandResult(exitCode, stdout.String(), stderr.String()), nil
}

// formatCommandResult renders the captured output blocks and the exit code in
// a stable, model-readable layout.
func formatCommandResult(exitCode int, stdout, stderr string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "exit code: %d\n", exitCode)
	builder.WriteString("stdout:\n")
	builder.WriteString(stdout)
	if !strings.HasSuffix(stdout, "\n") {
		builder.WriteByte('\n')
	}
	builder.WriteString("stderr:\n")
	builder.WriteString(stderr)
	if !strings.HasSuffix(stderr, "\n") {
		builder.WriteByte('\n')
	}
	return builder.String()
}

type lsTool struct{ filesystem *filesystem }

type lsArguments struct {
	Path string `json:"path"`
}

type editTool struct {
	filesystem *filesystem
}

type editArguments struct {
	Path  string        `json:"path"`
	Edits []edit.Change `json:"edits"`
}

func (t *editTool) Tier() approval.Tier { return approval.Write }

type writeTool struct {
	filesystem *filesystem
}

type writeArguments struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Overwrite bool   `json:"overwrite"`
}

func (t *writeTool) Tier() approval.Tier { return approval.Write }

func (t *writeTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "write",
			Description: "Create a repository file, populate an empty file, or replace a whole file's content. Parent directories are created as needed; an existing non-empty file is left unchanged unless overwrite is set.",
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Repository-relative file path to write.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Full file content to write.",
				},
				"overwrite": map[string]any{
					"type":        "boolean",
					"description": "Replace an existing non-empty file's content when true.",
				},
			}, "path", "content"),
		},
	}
}

func (t *writeTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var arguments writeArguments
	if err := decodeArguments(raw, &arguments); err != nil {
		return "", fmt.Errorf("write arguments: %w", err)
	}
	path, err := t.filesystem.resolveCreatePath(ctx, arguments.Path)
	if err != nil {
		return "", fmt.Errorf("write path: %w", err)
	}
	info, statErr := os.Stat(path)
	perm := os.FileMode(0o644)
	switch {
	case statErr == nil:
		if info.IsDir() {
			return "", fmt.Errorf("write %q: path is a directory", arguments.Path)
		}
		perm = info.Mode().Perm()
		if info.Size() > 0 && !arguments.Overwrite {
			return "", fmt.Errorf("write %q: file already exists with content; set overwrite to replace it or use edit for surgical changes", arguments.Path)
		}
	case errors.Is(statErr, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("write %q: create parent directories: %w", arguments.Path, err)
		}
	default:
		return "", fmt.Errorf("write %q: %w", arguments.Path, statErr)
	}
	if err := writeFileAtomic(path, []byte(arguments.Content), perm); err != nil {
		return "", fmt.Errorf("write %q: %w", arguments.Path, err)
	}
	return fmt.Sprintf("Wrote %s.", arguments.Path), nil
}

func (t *lsTool) Tier() approval.Tier { return approval.Read }

func (t *readTool) Tier() approval.Tier { return approval.Read }

func (t *grepTool) Tier() approval.Tier { return approval.Read }

func (t *editTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "edit",
			Description: "Apply exact-match replacements to a repository file. Every oldText must occur exactly once; if any match fails no file is changed.",
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Repository-relative file path to edit.",
				},
				"edits": map[string]any{
					"type":        "array",
					"description": "Exact search-and-replace pairs applied atomically.",
					"items": objectSchema(map[string]any{
						"oldText": map[string]any{
							"type":        "string",
							"description": "Exact text to find; must appear exactly once.",
						},
						"newText": map[string]any{
							"type":        "string",
							"description": "Replacement text.",
						},
					}, "oldText", "newText"),
				},
			}, "path", "edits"),
		},
	}
}

func (t *editTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var arguments editArguments
	if err := decodeArguments(raw, &arguments); err != nil {
		return "", fmt.Errorf("edit arguments: %w", err)
	}
	if len(arguments.Edits) == 0 {
		return "", errors.New("edit requires at least one replacement")
	}
	for index, change := range arguments.Edits {
		if strings.TrimSpace(change.OldText) == "" {
			return "", fmt.Errorf("edit %d: oldText is required", index+1)
		}
	}
	path, err := t.filesystem.resolve(ctx, arguments.Path, false)
	if err != nil {
		return "", fmt.Errorf("edit path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("edit %q: %w", arguments.Path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("edit %q: path is a directory", arguments.Path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("edit %q: %w", arguments.Path, err)
	}
	updated, err := edit.Apply(data, arguments.Edits)
	if err != nil {
		return "", fmt.Errorf("edit %q: %w", arguments.Path, err)
	}
	if err := writeFileAtomic(path, updated, info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("edit %q: %w", arguments.Path, err)
	}
	return fmt.Sprintf("Applied %d replacement(s) to %s.", len(arguments.Edits), arguments.Path), nil
}

// writeFileAtomic replaces path with data by writing to a temporary file in the
// same directory and renaming it into place, so a failed write never leaves a
// partially written target.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(perm); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
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

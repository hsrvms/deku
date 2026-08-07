package tool

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/provider"
)

func TestRegistryProvidesReadOnlyToolDefinitions(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	definitions := registry.Definitions()
	if got := definitionNames(definitions); !reflect.DeepEqual(got, []string{"command", "edit", "grep", "ls", "read", "write"}) {
		t.Fatalf("tool names = %#v, want command, edit, grep, ls, read, write", got)
	}
	for _, definition := range definitions {
		if definition.Type != "function" {
			t.Errorf("%s type = %q, want function", definition.Function.Name, definition.Type)
		}
		if definition.Function.Description == "" {
			t.Errorf("%s description is empty", definition.Function.Name)
		}
		parameters, ok := definition.Function.Parameters.(map[string]any)
		if !ok || parameters["type"] != "object" {
			t.Errorf("%s parameters = %#v, want object schema", definition.Function.Name, definition.Function.Parameters)
		}
	}
}

func TestRegistryExecutesReadOnlyToolsWithinRepository(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "main.go"), []byte("package pkg\n\nfunc Answer() int {\n\treturn 42\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	ls, err := registry.Execute(context.Background(), "ls", `{ "path": "pkg" }`)
	if err != nil {
		t.Fatalf("ls error = %v", err)
	}
	if ls != "main.go\n" {
		t.Errorf("ls result = %q, want main.go listing", ls)
	}

	read, err := registry.Execute(context.Background(), "read", `{ "path": "pkg/main.go", "start_line": 3, "end_line": 3 }`)
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	if read != "func Answer() int {\n" {
		t.Errorf("read result = %q, want selected line", read)
	}

	grep, err := registry.Execute(context.Background(), "grep", `{ "pattern": "return [0-9]+", "path": "pkg" }`)
	if err != nil {
		t.Fatalf("grep error = %v", err)
	}
	if grep != "pkg/main.go:4:\treturn 42\n" {
		t.Errorf("grep result = %q, want matching line", grep)
	}
}

func TestRegistryRejectsInvalidArgumentsAndPathTraversal(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	cases := []struct {
		name string
		args string
	}{
		{name: "unknown tool", args: `{}`},
		{name: "invalid JSON", args: `{`},
		{name: "missing read path", args: `{}`},
		{name: "path traversal", args: `{ "path": "../outside" }`},
	}
	for _, testCase := range cases {
		var toolName string
		switch testCase.name {
		case "unknown tool":
			toolName = "unknown"
		case "invalid JSON":
			toolName = "read"
		case "missing read path", "path traversal":
			toolName = "read"
		}
		if _, err := registry.Execute(context.Background(), toolName, testCase.args); err == nil {
			t.Errorf("%s: Execute() error = nil, want error", testCase.name)
		}
	}
}

func TestEditToolAppliesExactReplacementsAtomically(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	result, err := registry.Execute(context.Background(), "edit", `{
		"path": "main.go",
		"edits": [
			{"oldText": "package main", "newText": "package app"},
			{"oldText": "func main() {}", "newText": "func run() {}"}
		]
	}`)
	if err != nil {
		t.Fatalf("edit error = %v", err)
	}
	if result != "Applied 2 replacement(s) to main.go." {
		t.Errorf("edit result = %q", result)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package app\n\nfunc run() {}\n" {
		t.Errorf("edited file = %q, want replaced content", got)
	}
}

func TestEditToolFailsAtomicallyOnMismatch(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "main.go")
	original := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	_, err = registry.Execute(context.Background(), "edit", `{
		"path": "main.go",
		"edits": [
			{"oldText": "package main", "newText": "package app"},
			{"oldText": "func absent()", "newText": "func added()"}
		]
	}`)
	if err == nil {
		t.Fatal("edit error = nil, want mismatch error")
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("file mutated after failure = %q, want %q", got, original)
	}
}

func TestEditToolRejectsEmptyEditsAndPathTraversal(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	cases := []string{
		`{"path":"main.go","edits":[]}`,
		`{"path":"main.go"}`,
		`{"path":"../outside","edits":[{"oldText":"a","newText":"b"}]}`,
	}
	for _, args := range cases {
		if _, err := registry.Execute(context.Background(), "edit", args); err == nil {
			t.Errorf("edit %s: Execute() error = nil, want error", args)
		}
	}
}

func TestRegistryRendersCommandReports(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	cases := []struct {
		name string
		tool string
		args string
		want string
	}{
		{name: "command", tool: "command", args: `{"command":"go test ./..."}`, want: "Run: go test ./..."},
		{name: "command with dir", tool: "command", args: `{"command":"make build","dir":"pkg"}`, want: "Run: make build (in pkg)"},
		{name: "edit", tool: "edit", args: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func run() {}"}]}`, want: "Edit: main.go\n  replace \"func main() {}\" with \"func run() {}\""},
		{name: "write", tool: "write", args: `{"path":"notes.txt","content":"hello"}`, want: "Write: notes.txt"},
		{name: "write overwrite", tool: "write", args: `{"path":"main.go","content":"x","overwrite":true}`, want: "Write: main.go (overwrite)"},
		{name: "read", tool: "read", args: `{"path":"main.go"}`, want: "Read: main.go"},
		{name: "read line range", tool: "read", args: `{"path":"main.go","start_line":3,"end_line":5}`, want: "Read: main.go (lines 3-5)"},
		{name: "read start line only", tool: "read", args: `{"path":"main.go","start_line":3}`, want: "Read: main.go (from line 3)"},
		{name: "ls", tool: "ls", args: `{"path":"pkg"}`, want: "List: pkg"},
		{name: "ls root", tool: "ls", args: `{}`, want: "List: repository root"},
		{name: "grep", tool: "grep", args: `{"pattern":"TODO","path":"pkg"}`, want: "Search: TODO in pkg"},
		{name: "grep without path", tool: "grep", args: `{"pattern":"TODO"}`, want: "Search: TODO"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := registry.Report(tc.tool, tc.args)
			if err != nil {
				t.Fatalf("Report(%q) error = %v", tc.tool, err)
			}
			if got != tc.want {
				t.Errorf("Report(%q) = %q, want %q", tc.tool, got, tc.want)
			}
		})
	}
}

func TestRegistryRefusesUnrenderableCommandReports(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	cases := []struct {
		name string
		tool string
		args string
	}{
		{name: "unknown tool", tool: "unknown", args: `{}`},
		{name: "command without command", tool: "command", args: `{"dir":"pkg"}`},
		{name: "command malformed arguments", tool: "command", args: `{"command":42}`},
		{name: "command negative timeout", tool: "command", args: `{"command":"ls","timeout":-1}`},
		{name: "edit without edits", tool: "edit", args: `{"path":"main.go"}`},
		{name: "edit empty oldText", tool: "edit", args: `{"path":"main.go","edits":[{"oldText":"","newText":"x"}]}`},
		{name: "edit without path", tool: "edit", args: `{"edits":[{"oldText":"a","newText":"b"}]}`},
		{name: "write without path", tool: "write", args: `{"content":"x"}`},
		{name: "read without path", tool: "read", args: `{}`},
		{name: "read negative start line", tool: "read", args: `{"path":"main.go","start_line":0}`},
		{name: "grep without pattern", tool: "grep", args: `{"path":"pkg"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := registry.Report(tc.tool, tc.args); err == nil {
				t.Errorf("Report(%q, %s) error = nil, want error", tc.tool, tc.args)
			}
		})
	}
}

func TestRegistryDeclaresToolTiers(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	cases := map[string]approval.Tier{
		"command": approval.Destructive,
		"ls":      approval.Read,
		"read":    approval.Read,
		"grep":    approval.Read,
		"edit":    approval.Write,
		"write":   approval.Write,
	}
	for name, want := range cases {
		got, tierErr := registry.Tier(name)
		if tierErr != nil {
			t.Fatalf("Tier(%q) error = %v", name, tierErr)
		}
		if got != want {
			t.Errorf("Tier(%q) = %q, want %q", name, got, want)
		}
	}
	if _, tierErr := registry.Tier("unknown"); tierErr == nil {
		t.Errorf("Tier(unknown) error = nil, want error")
	}
}

func TestWriteToolCreatesNewFile(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	result, err := registry.Execute(context.Background(), "write", `{"path":"main.go","content":"package main\n\nfunc main() {}\n"}`)
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	if result != "Wrote main.go." {
		t.Errorf("write result = %q", result)
	}
	got, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc main() {}\n" {
		t.Errorf("written file = %q", got)
	}
}

func TestWriteToolCreatesNestedFileWithParentDirectories(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	if _, err := registry.Execute(context.Background(), "write", `{"path":"src/index.html","content":"<title>Deku</title>\n"}`); err != nil {
		t.Fatalf("write error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "src", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "<title>Deku</title>\n" {
		t.Errorf("written file = %q", got)
	}
}

func TestWriteToolPopulatesEmptyFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "placeholder.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	if _, err := registry.Execute(context.Background(), "write", `{"path":"placeholder.txt","content":"filled\n"}`); err != nil {
		t.Fatalf("write error = %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "filled\n" {
		t.Errorf("populated file = %q", got)
	}
}

func TestWriteToolRefusesNonEmptyOverwriteWithoutFlag(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	original := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(file, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	_, err = registry.Execute(context.Background(), "write", `{"path":"main.go","content":"clobbered\n"}`)
	if err == nil {
		t.Fatal("write error = nil, want refusal for non-empty target")
	}
	got, readErr := os.ReadFile(file)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("file mutated after refusal = %q, want %q", got, original)
	}
}

func TestWriteToolHonorsOverwriteFlag(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	if _, err := registry.Execute(context.Background(), "write", `{"path":"main.go","content":"replaced\n","overwrite":true}`); err != nil {
		t.Fatalf("write error = %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "replaced\n" {
		t.Errorf("overwritten file = %q", got)
	}
}

func TestWriteToolRejectsPathTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	cases := []string{
		`{"path":"../outside.txt","content":"x"}`,
		`{"path":"a/../../escape.txt","content":"x"}`,
		`{"path":"/absolute/path.txt","content":"x"}`,
	}
	for _, args := range cases {
		if _, err := registry.Execute(context.Background(), "write", args); err == nil {
			t.Errorf("write %s: Execute() error = nil, want error", args)
		}
	}

	// A symlink inside the root pointing outside must not be escapable.
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "escape")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := registry.Execute(context.Background(), "write", `{"path":"escape/victim.txt","content":"x"}`); err == nil {
		t.Error("write through escaping symlink: Execute() error = nil, want error")
	}
	if _, err := os.Stat(filepath.Join(external, "victim.txt")); !os.IsNotExist(err) {
		t.Errorf("write escaped repository root; external file exists or stat failed: %v", err)
	}
}

func TestCommandToolRunsAndCapturesOutput(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	result, err := registry.Execute(context.Background(), "command", `{"command":"printf 'hello\\n'"}`)
	if err != nil {
		t.Fatalf("command error = %v", err)
	}
	if !strings.Contains(result, "exit code: 0") {
		t.Errorf("command result = %q, want exit code 0", result)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("command result = %q, want captured stdout", result)
	}
}

func TestCommandToolHonorsWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	if _, err := registry.Execute(context.Background(), "command", `{"command":"echo built > built.txt","dir":"pkg"}`); err != nil {
		t.Fatalf("command error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "pkg", "built.txt"))
	if err != nil {
		t.Fatalf("command did not run in working directory: %v", err)
	}
	if string(got) != "built\n" {
		t.Errorf("command output file = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "built.txt")); !os.IsNotExist(err) {
		t.Errorf("command leaked outside working directory: %v", err)
	}
}

func TestCommandToolReportsNonZeroExitCode(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	result, err := registry.Execute(context.Background(), "command", `{"command":"printf 'oops\\n'; exit 3"}`)
	if err != nil {
		t.Fatalf("command error = %v", err)
	}
	if !strings.Contains(result, "exit code: 3") {
		t.Errorf("command result = %q, want exit code 3", result)
	}
	if !strings.Contains(result, "oops") {
		t.Errorf("command result = %q, want captured output on failure", result)
	}
}

func TestCommandToolHonorsTimeout(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	_, err = registry.Execute(context.Background(), "command", `{"command":"sleep 5","timeout":1}`)
	if err == nil {
		t.Fatal("command error = nil, want timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("command error = %q, want timeout report", err.Error())
	}
}

func TestCommandToolRejectsEmptyCommandAndPathTraversal(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	cases := []string{
		`{}`,
		`{"command":"  "}`,
		`{"command":"echo hi","dir":"../outside"}`,
		`{"command":"echo hi","dir":"/absolute"}`,
		`{"command":"echo hi","timeout":-1}`,
	}
	for _, args := range cases {
		if _, err := registry.Execute(context.Background(), "command", args); err == nil {
			t.Errorf("command %s: Execute() error = nil, want error", args)
		}
	}
}

func definitionNames(definitions []provider.ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Function.Name)
	}
	return names
}

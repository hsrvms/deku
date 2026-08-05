package tool

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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
	if got := definitionNames(definitions); !reflect.DeepEqual(got, []string{"edit", "grep", "ls", "read", "write"}) {
		t.Fatalf("tool names = %#v, want edit, grep, ls, read, write", got)
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

func TestRegistryDeclaresToolTiers(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	cases := map[string]approval.Tier{
		"ls":    approval.Read,
		"read":  approval.Read,
		"grep":  approval.Read,
		"edit":  approval.Write,
		"write": approval.Write,
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

func definitionNames(definitions []provider.ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Function.Name)
	}
	return names
}

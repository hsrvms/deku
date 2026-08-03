package tool

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hsrvms/deku/provider"
)

func TestRegistryProvidesReadOnlyToolDefinitions(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	definitions := registry.Definitions()
	if got := definitionNames(definitions); !reflect.DeepEqual(got, []string{"grep", "ls", "read"}) {
		t.Fatalf("tool names = %#v, want grep, ls, read", got)
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

func definitionNames(definitions []provider.ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Function.Name)
	}
	return names
}

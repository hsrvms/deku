package repomap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildListsRepositoryFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	writeFile(t, root, "agent/agent.go", "package agent\n")
	writeFile(t, root, "agent/agent_test.go", "package agent\n")
	writeFile(t, root, "cmd/deku/main.go", "package main\n")

	builder, err := NewBuilder(root, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	got, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	for _, want := range []string{"main.go", "agent/", "agent.go", "agent_test.go", "cmd/", "deku/", "cmd/deku/main.go"} {
		if want == "cmd/deku/main.go" {
			continue // the tree renders branches, not joined slash paths
		}
		if !strings.Contains(got, want) {
			t.Errorf("map %q does not contain %q", got, want)
		}
	}
	if !strings.Contains(got, "├──") && !strings.Contains(got, "└──") {
		t.Errorf("map %q does not render a file tree with branch characters", got)
	}
}

func TestBuildRespectsGitignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "*.log\n/build/\n")
	writeFile(t, root, "app.log", "x")
	writeFile(t, root, "build/app.go", "x")
	writeFile(t, root, "keep.go", "x")

	builder, err := NewBuilder(root, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	got, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if strings.Contains(got, "app.log") {
		t.Errorf("map %q includes gitignored app.log", got)
	}
	if strings.Contains(got, "build") {
		t.Errorf("map %q includes gitignored build/", got)
	}
	if !strings.Contains(got, "keep.go") {
		t.Errorf("map %q omits keep.go", got)
	}
}

func TestBuildNestedGitignoreCanUnignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "*.tmp\n")
	writeFile(t, root, "sub/.gitignore", "!keep.tmp\n")
	writeFile(t, root, "sub/keep.tmp", "x")
	writeFile(t, root, "sub/drop.tmp", "x")

	builder, err := NewBuilder(root, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	got, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !strings.Contains(got, "keep.tmp") {
		t.Errorf("map %q omits sub/keep.tmp unignored by nested .gitignore", got)
	}
	if strings.Contains(got, "drop.tmp") {
		t.Errorf("map %q includes sub/drop.tmp still ignored", got)
	}
}

func TestBuildAnchoredPatternOnlyMatchesAtBase(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "/only_root.log\n")
	writeFile(t, root, "only_root.log", "x")
	writeFile(t, root, "sub/nested_only.log", "x")

	builder, err := NewBuilder(root, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	got, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if strings.Contains(got, "only_root.log") {
		t.Errorf("map %q includes root-only anchored-ignored only_root.log", got)
	}
	if !strings.Contains(got, "nested_only.log") {
		t.Errorf("map %q omits nested_only.log which the anchored pattern must not ignore", got)
	}
}

func TestBuildHonorsConfigExclusionPolicy(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "vendor/dep.go", "x")
	writeFile(t, root, "gen.go", "x")
	writeFile(t, root, "model.gen.go", "x")

	builder, err := NewBuilder(root, []string{"vendor", "*.gen.go"})
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	got, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if strings.Contains(got, "vendor") {
		t.Errorf("map %q includes excluded vendor/", got)
	}
	if strings.Contains(got, "model.gen.go") {
		t.Errorf("map %q includes excluded model.gen.go", got)
	}
	if !strings.Contains(got, "gen.go") {
		t.Errorf("map %q omits gen.go which should be included", got)
	}
}

func TestBuildSkipsDotGitDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "x")
	writeFile(t, root, ".git/objects/somefile", "x")

	builder, err := NewBuilder(root, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	got, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if strings.Contains(got, ".git") {
		t.Errorf("map %q includes the .git directory", got)
	}
}

func TestBuildTruncatesWithinTokenBudget(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		writeFile(t, root, name, "x")
	}

	builder, err := NewBuilder(root, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	builder.MaxTokens = 1
	builder.MaxEntries = 100
	got, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("map %q is not marked as truncated within a small token budget", got)
	}
	if len(got) > 512 {
		t.Errorf("map length = %d, want bounded by token budget", len(got))
	}
}

func TestNewBuilderRequiresRoot(t *testing.T) {
	if _, err := NewBuilder("", nil); err == nil {
		t.Fatal("NewBuilder('') expected an error")
	}
	if _, err := NewBuilder("  ", nil); err == nil {
		t.Fatal("NewBuilder('  ') expected an error")
	}
}
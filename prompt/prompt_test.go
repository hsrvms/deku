package prompt

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptContainsBaseInstructions(t *testing.T) {
	got := BuildSystemPrompt("")
	if strings.TrimSpace(got) == "" {
		t.Fatal("BuildSystemPrompt() returned an empty prompt")
	}
	for _, want := range []string{
		"Deku",
		"coding agent",
		"multiple Steps",
		"read-only tools",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt %q does not contain %q", got, want)
		}
	}
}

func TestBuildSystemPromptWithRepoMapIncludesInstructionAndMap(t *testing.T) {
	const repoMap = "├── main.go\n└── agent/\n    └── agent.go\n"
	got := BuildSystemPrompt(repoMap)
	for _, want := range []string{
		"Repository Map",
		"The map shows file paths, not source code.",
		"Use `read` to obtain file contents before editing.",
		"main.go",
		"agent.go",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt %q does not contain %q", got, want)
		}
	}
}

func TestBuildSystemPromptWithoutMapOmitsMapSection(t *testing.T) {
	got := BuildSystemPrompt("")
	if strings.Contains(got, "Repository Map") {
		t.Errorf("system prompt %q should omit the Repository Map section when no map is provided", got)
	}
}

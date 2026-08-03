package prompt

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptContainsBaseInstructions(t *testing.T) {
	got := BuildSystemPrompt()
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

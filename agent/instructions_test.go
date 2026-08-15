package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/prompt"
	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/repository"
	"github.com/hsrvms/deku/session"
	"github.com/hsrvms/deku/tool"
)

// writeAgentHome points HOME at a fresh temporary directory holding the
// given Deku Home files, following the temp-Deku-Home pattern of the config
// and CLI tests, and returns the Deku Home directory. Instruction files are
// discovered from the Deku Home, so the test controls them exactly like the
// production loader does.
func writeAgentHome(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dekuDir := filepath.Join(home, ".deku")
	if err := os.MkdirAll(dekuDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if body == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dekuDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dekuDir
}

// turnRecordsSystem runs one completed Turn with the instruction set loaded
// from dekuHome for the Repository at root (a fresh seeded fixture when root
// is empty) and returns the system string the scripted Provider recorded.
// The Turn requests an introduction and the model answers with text only, so
// the recorded system string is the complete System Prompt of the Turn's
// Step, observed through the Agent seam.
func turnRecordsSystem(t *testing.T, dekuHome, root string, trusted bool) string {
	t.Helper()
	if root == "" {
		root = seedFixture(t)
	}
	instructions, err := prompt.LoadInstructions(dekuHome, root, trusted)
	if err != nil {
		t.Fatalf("LoadInstructions() error = %v", err)
	}
	registry, err := tool.NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	selection := provider.Selection{Provider: "first", Model: "model-a"}
	stub := respondingProvider("Hello from the fixture.")
	source := &scriptedSelectionSource{adapters: map[provider.Selection]*scriptedProvider{selection: stub}}
	var output bytes.Buffer
	runner, err := NewWithSelectionAndActivity(source, selection, conversation, &output, nil, registry, approval.DefaultPolicy(), nil, nil, repository.ModeOff, "", nil, instructions)
	if err != nil {
		t.Fatalf("NewWithSelectionAndActivity() error = %v", err)
	}
	if _, err := runner.Turn(context.Background(), "Introduce yourself."); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if stub.system == "" {
		t.Fatal("scripted provider recorded no system prompt")
	}
	return stub.system
}

func TestTurnAppliesGlobalAndProjectInstructions(t *testing.T) {
	root := seedFixture(t)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("PROJECT-INSTRUCTIONS-MARKER"), 0o644); err != nil {
		t.Fatal(err)
	}
	dekuHome := writeAgentHome(t, map[string]string{"AGENTS.md": "GLOBAL-INSTRUCTIONS-MARKER"})
	system := turnRecordsSystem(t, dekuHome, root, true)
	for _, want := range []string{
		"GLOBAL-INSTRUCTIONS-MARKER",
		"PROJECT-INSTRUCTIONS-MARKER",
		"Repository Map",
		"The map shows file paths, not source code.",
		"mean.go",
		"counter.go",
	} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt %q does not contain %q", system, want)
		}
	}
	global := strings.Index(system, "GLOBAL-INSTRUCTIONS-MARKER")
	project := strings.Index(system, "PROJECT-INSTRUCTIONS-MARKER")
	machinery := strings.Index(system, "Repository Map")
	if global >= project || project >= machinery {
		t.Errorf("layer order is wrong: global=%d, project=%d, machinery=%d", global, project, machinery)
	}
}

func TestTurnAppliesProjectInstructionsRegardlessOfTrust(t *testing.T) {
	root := seedFixture(t)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("PROJECT-INSTRUCTIONS-MARKER"), 0o644); err != nil {
		t.Fatal(err)
	}
	dekuHome := writeAgentHome(t, nil)
	trusted := turnRecordsSystem(t, dekuHome, root, true)
	if !strings.Contains(trusted, "PROJECT-INSTRUCTIONS-MARKER") {
		t.Errorf("trusted system prompt %q lacks the Project Instructions", trusted)
	}
	untrusted := turnRecordsSystem(t, dekuHome, root, false)
	if !strings.Contains(untrusted, "PROJECT-INSTRUCTIONS-MARKER") {
		t.Errorf("untrusted system prompt %q lacks the Project Instructions", untrusted)
	}
	if trusted != untrusted {
		t.Errorf("trust and no-trust system prompts differ:\n%q\n%q", trusted, untrusted)
	}
}

func TestTurnOverrideReplacesBaseSystemPrompt(t *testing.T) {
	root := seedFixture(t)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("PROJECT-INSTRUCTIONS-MARKER"), 0o644); err != nil {
		t.Fatal(err)
	}
	dekuHome := writeAgentHome(t, map[string]string{
		"AGENTS.md": "GLOBAL-INSTRUCTIONS-MARKER",
		"SYSTEM.md": "OVERRIDE-MARKER",
	})
	system := turnRecordsSystem(t, dekuHome, root, true)
	if strings.Contains(system, "helpful terminal-first coding agent") {
		t.Errorf("base identity text survives an override: %q", system)
	}
	for _, want := range []string{
		"OVERRIDE-MARKER",
		"GLOBAL-INSTRUCTIONS-MARKER",
		"PROJECT-INSTRUCTIONS-MARKER",
		"Repository Map",
		"mean.go",
		"counter.go",
	} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt %q does not contain %q", system, want)
		}
	}
}

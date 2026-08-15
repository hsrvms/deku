package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSystemPromptContainsBaseInstructions(t *testing.T) {
	got := BuildSystemPrompt("", nil)
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
	got := BuildSystemPrompt(repoMap, nil)
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
	got := BuildSystemPrompt("", nil)
	if strings.Contains(got, "Repository Map") {
		t.Errorf("system prompt %q should omit the Repository Map section when no map is provided", got)
	}
}

func TestBuildSystemPromptLayersInOrder(t *testing.T) {
	instructions := &Instructions{
		Global:  "GLOBAL-MARKER",
		Project: "PROJECT-MARKER",
	}
	got := BuildSystemPrompt("", instructions)
	base := strings.Index(got, "helpful terminal-first coding agent")
	global := strings.Index(got, "GLOBAL-MARKER")
	project := strings.Index(got, "PROJECT-MARKER")
	if base < 0 || global < 0 || project < 0 {
		t.Fatalf("system prompt %q lacks one of the base, Global, or Project layers", got)
	}
	if base >= global || global >= project {
		t.Errorf("layer order is wrong: base=%d, global=%d, project=%d", base, global, project)
	}
}

func TestBuildSystemPromptOverrideReplacesBase(t *testing.T) {
	got := BuildSystemPrompt("", &Instructions{Override: "Speak as a laconic pirate."})
	if strings.Contains(got, "helpful terminal-first coding agent") {
		t.Errorf("base identity text survives an override: %q", got)
	}
	if !strings.Contains(got, "Speak as a laconic pirate.") {
		t.Errorf("system prompt %q does not contain the override text", got)
	}
}

func TestBuildSystemPromptOverrideCoexistsWithAdditionsAndMachinery(t *testing.T) {
	const repoMap = "stats/mean.go\n... (map truncated to stay within 1000 tokens)\n"
	instructions := &Instructions{
		Override: "OVERRIDE-MARKER",
		Global:   "GLOBAL-MARKER",
		Project:  "PROJECT-MARKER",
	}
	got := BuildSystemPrompt(repoMap, instructions)
	for _, want := range []string{
		"OVERRIDE-MARKER",
		"GLOBAL-MARKER",
		"PROJECT-MARKER",
		"Repository Map",
		"The map shows file paths, not source code.",
		"stats/mean.go",
		"map truncated",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "helpful terminal-first coding agent") {
		t.Errorf("base identity text survives an override: %q", got)
	}
	override := strings.Index(got, "OVERRIDE-MARKER")
	global := strings.Index(got, "GLOBAL-MARKER")
	project := strings.Index(got, "PROJECT-MARKER")
	machinery := strings.Index(got, "Repository Map")
	if override >= global || global >= project || project >= machinery {
		t.Errorf("layer order is wrong: override=%d, global=%d, project=%d, machinery=%d", override, global, project, machinery)
	}
}

func TestBuildSystemPromptWhitespaceOnlyLayersContributeNothing(t *testing.T) {
	blank := BuildSystemPrompt("", &Instructions{Override: "  \n", Global: "\t", Project: "   "})
	if blank != BuildSystemPrompt("", nil) {
		t.Errorf("whitespace-only layers changed the prompt: %q", blank)
	}
}

func TestLoadInstructionsReadsAllThreeFiles(t *testing.T) {
	home := writeInstructionHome(t, map[string]string{
		"AGENTS.md": "global instructions",
		"SYSTEM.md": "override text",
	})
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("project instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	instructions, err := LoadInstructions(home, project, false)
	if err != nil {
		t.Fatalf("LoadInstructions() error = %v", err)
	}
	if instructions == nil {
		t.Fatal("LoadInstructions() returned nil")
	}
	if instructions.Global != "global instructions" {
		t.Errorf("Global = %q, want %q", instructions.Global, "global instructions")
	}
	if instructions.Override != "override text" {
		t.Errorf("Override = %q, want %q", instructions.Override, "override text")
	}
	if instructions.Project != "project instructions" {
		t.Errorf("Project = %q, want %q", instructions.Project, "project instructions")
	}
}

func TestLoadInstructionsMissingFilesAreAbsent(t *testing.T) {
	instructions, err := LoadInstructions(t.TempDir(), "", false)
	if err != nil {
		t.Fatalf("LoadInstructions() error = %v", err)
	}
	if instructions == nil {
		t.Fatal("LoadInstructions() returned nil")
	}
	if instructions.Override != "" || instructions.Global != "" || instructions.Project != "" {
		t.Errorf("missing files should yield absent layers, got %+v", instructions)
	}
}

func TestLoadInstructionsEmptyProjectRootYieldsNoProjectLayer(t *testing.T) {
	home := writeInstructionHome(t, map[string]string{"AGENTS.md": "global instructions"})
	instructions, err := LoadInstructions(home, "", true)
	if err != nil {
		t.Fatalf("LoadInstructions() error = %v", err)
	}
	if instructions.Global != "global instructions" {
		t.Errorf("Global = %q, want %q", instructions.Global, "global instructions")
	}
	if instructions.Project != "" {
		t.Errorf("Project = %q, want empty for an empty project root", instructions.Project)
	}
}

func TestLoadInstructionsProjectFileReadsRegardlessOfTrust(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("repo conventions"), 0o644); err != nil {
		t.Fatal(err)
	}
	trusted, err := LoadInstructions(t.TempDir(), project, true)
	if err != nil {
		t.Fatalf("LoadInstructions(trusted) error = %v", err)
	}
	untrusted, err := LoadInstructions(t.TempDir(), project, false)
	if err != nil {
		t.Fatalf("LoadInstructions(untrusted) error = %v", err)
	}
	if trusted.Project != "repo conventions" {
		t.Errorf("trusted Project = %q, want %q", trusted.Project, "repo conventions")
	}
	if untrusted.Project != "repo conventions" {
		t.Errorf("untrusted Project = %q, want %q", untrusted.Project, "repo conventions")
	}
	if trusted.Project != untrusted.Project {
		t.Errorf("trust and no-trust Project layers differ: %q vs %q", trusted.Project, untrusted.Project)
	}
}

func TestLoadInstructionsUnreadableFileFailsNamingFile(t *testing.T) {
	cases := []struct {
		name       string
		prepare    func(t *testing.T) (home, project string)
		wantNaming string
	}{
		{
			name: "Deku Home AGENTS.md",
			prepare: func(t *testing.T) (string, string) {
				home := t.TempDir()
				if err := os.MkdirAll(filepath.Join(home, "AGENTS.md"), 0o755); err != nil {
					t.Fatal(err)
				}
				return home, ""
			},
			wantNaming: "AGENTS.md",
		},
		{
			name: "Deku Home SYSTEM.md",
			prepare: func(t *testing.T) (string, string) {
				home := t.TempDir()
				if err := os.MkdirAll(filepath.Join(home, "SYSTEM.md"), 0o755); err != nil {
					t.Fatal(err)
				}
				return home, ""
			},
			wantNaming: "SYSTEM.md",
		},
		{
			name: "Repository root AGENTS.md",
			prepare: func(t *testing.T) (string, string) {
				project := t.TempDir()
				if err := os.MkdirAll(filepath.Join(project, "AGENTS.md"), 0o755); err != nil {
					t.Fatal(err)
				}
				return t.TempDir(), project
			},
			wantNaming: "AGENTS.md",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, project := tc.prepare(t)
			_, err := LoadInstructions(home, project, false)
			if err == nil {
				t.Fatal("LoadInstructions() succeeded for an unreadable file")
			}
			if !strings.Contains(err.Error(), tc.wantNaming) {
				t.Errorf("error %q does not name the file %q", err, tc.wantNaming)
			}
		})
	}
}

func TestLoadInstructionsEmptyFilesContributeNothing(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"AGENTS.md", "SYSTEM.md"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte("  \n\t"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	instructions, err := LoadInstructions(home, project, true)
	if err != nil {
		t.Fatalf("LoadInstructions() error = %v", err)
	}
	if instructions.Override != "" || instructions.Global != "" || instructions.Project != "" {
		t.Errorf("blank files should yield absent layers, got %+v", instructions)
	}
}

// writeInstructionHome writes instruction files into a fresh temporary Deku
// Home directory, skipping empty bodies, and returns the directory. It is the
// prompt package's counterpart of the config package's writeDekuHome helper.
func writeInstructionHome(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	for name, body := range files {
		if body == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(home, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

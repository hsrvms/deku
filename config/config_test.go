package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hsrvms/deku/provider"
)

// writeDekuHome writes Deku Home files (settings.json, auth.json,
// models.json, .env) for the current test, pointing $HOME at a fresh
// temporary directory that contains them all. An empty body skips that file,
// so a missing module is simply absent.
func writeDekuHome(t *testing.T, files map[string]string) {
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
}

// writeModules writes Deku Home module files, skipping empty bodies.
func writeModules(t *testing.T, settings, auth, models string) {
	t.Helper()
	writeDekuHome(t, map[string]string{
		"settings.json": settings,
		"auth.json":     auth,
		"models.json":   models,
	})
}

// writeEnv writes a Deku Home .env file for the current test.
func writeEnv(t *testing.T, body string) {
	t.Helper()
	writeDekuHome(t, map[string]string{".env": body})
}

// standardModels is a models module declaring two named Providers, used by
// tests that need a populated Provider Registry.
const standardModels = `{
  "providers": {
    "first": {
      "adapter": "openai-compatible",
      "base_url": "https://api.first.example.com/v1",
      "auth": "first-auth",
      "models": ["model-a", "model-b"]
    },
    "second": {
      "adapter": "openai-compatible",
      "base_url": "https://api.second.example.com/v1",
      "auth": "second-auth",
      "models": ["model-c"]
    }
  }
}`

// standardAuth is an auth module declaring credentials for standardModels.
const standardAuth = `{
  "first-auth": { "type": "api_key", "api_key": "sk-first" },
  "second-auth": { "type": "api_key", "api_key": "sk-second" }
}`

func TestLoadNoModulesIsOk(t *testing.T) {
	writeModules(t, ``, ``, ``)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error when no module files exist: %v", err)
	}
	if cfg.Providers != nil {
		t.Errorf("providers = %#v, want none without a models module", cfg.Providers)
	}
	if cfg.Auth != nil {
		t.Errorf("auth = %#v, want none without an auth module", cfg.Auth)
	}
	if !cfg.Selection.IsZero() {
		t.Errorf("selection = %#v, want zero without settings defaults", cfg.Selection)
	}
}

func TestLoadInvalidJSONFails(t *testing.T) {
	writeModules(t, `{ not valid json`, ``, ``)

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for malformed settings.json")
	}
	if !strings.Contains(err.Error(), "settings.json") {
		t.Errorf("error = %q, want it to name settings.json", err)
	}

	writeModules(t, ``, `{ not valid json`, ``)
	_, err = Load("")
	if err == nil {
		t.Fatal("expected error for malformed auth.json")
	}
	if !strings.Contains(err.Error(), "auth.json") {
		t.Errorf("error = %q, want it to name auth.json", err)
	}

	writeModules(t, ``, ``, `{ not valid json`)
	_, err = Load("")
	if err == nil {
		t.Fatal("expected error for malformed models.json")
	}
	if !strings.Contains(err.Error(), "models.json") {
		t.Errorf("error = %q, want it to name models.json", err)
	}
}

func TestLoadApprovalOverridesFromSettings(t *testing.T) {
	writeModules(t,
		`{
  "approval": {
    "tools": { "edit": "destructive" },
    "defaults": { "read": "prompt", "write": "auto" }
  }
}`, ``, ``)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Approval.Tools["edit"]; got != "destructive" {
		t.Errorf("approval.tools.edit = %q, want destructive", got)
	}
	if got := cfg.Approval.Defaults["read"]; got != "prompt" {
		t.Errorf("approval.defaults.read = %q, want prompt", got)
	}
	if got := cfg.Approval.Defaults["write"]; got != "auto" {
		t.Errorf("approval.defaults.write = %q, want auto", got)
	}
}

func TestLoadRepoMapExcludeFromSettings(t *testing.T) {
	writeModules(t,
		`{
  "repo_map": {
    "exclude": ["vendor", "*.gen.go"]
  }
}`, ``, ``)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.RepoMap.Exclude; !reflect.DeepEqual(got, []string{"vendor", "*.gen.go"}) {
		t.Errorf("repo_map.exclude = %#v, want [vendor *.gen.go]", got)
	}
}

func TestAgentCommitsDefaults(t *testing.T) {
	writeModules(t, ``, ``, ``)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "off" {
		t.Errorf("agent_commits.mode = %q, want off default", cfg.AgentCommits.Mode)
	}
	if cfg.AgentCommits.Validation != "go test ./..." {
		t.Errorf("agent_commits.validation = %q, want go test ./...", cfg.AgentCommits.Validation)
	}
}

func TestAgentCommitsFromSettingsAndEnv(t *testing.T) {
	writeModules(t,
		`{
  "agent_commits": {
    "mode": "ask",
    "validation": "make test"
  }
}`, ``, ``)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "ask" {
		t.Errorf("agent_commits.mode = %q, want ask from settings", cfg.AgentCommits.Mode)
	}
	if cfg.AgentCommits.Validation != "make test" {
		t.Errorf("agent_commits.validation = %q, want make test from settings", cfg.AgentCommits.Validation)
	}

	// Environment variable overrides the settings mode.
	t.Setenv("DEKU_AGENT_COMMITS", "on")
	cfg, err = Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "on" {
		t.Errorf("agent_commits.mode = %q, want on from env override", cfg.AgentCommits.Mode)
	}
}

// --- Issue #39/#41 acceptance: Provider Registry and Selection declaration ---

func TestLoadParsesNamedProviders(t *testing.T) {
	writeModules(t, ``, standardAuth, standardModels)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("providers = %#v, want two named entries", cfg.Providers)
	}
	first, ok := cfg.Providers["first"]
	if !ok {
		t.Fatalf("providers = %#v, want a provider named first", cfg.Providers)
	}
	if first.Name != "first" {
		t.Errorf("first.Name = %q, want the map key", first.Name)
	}
	if first.Adapter != "openai-compatible" {
		t.Errorf("first.Adapter = %q, want openai-compatible", first.Adapter)
	}
	if first.BaseURL != "https://api.first.example.com/v1" {
		t.Errorf("first.BaseURL = %q, want the declared base URL", first.BaseURL)
	}
	if first.Auth != "first-auth" {
		t.Errorf("first.Auth = %q, want the authentication name reference", first.Auth)
	}
	if want := []string{"model-a", "model-b"}; !reflect.DeepEqual(first.Models, want) {
		t.Errorf("first.Models = %#v, want %#v", first.Models, want)
	}
	if _, ok := cfg.Providers["second"]; !ok {
		t.Errorf("providers = %#v, want a provider named second", cfg.Providers)
	}
}

func TestLoadParsesNamedAuthentication(t *testing.T) {
	writeModules(t, ``, standardAuth, standardModels)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	first, ok := cfg.Auth["first-auth"]
	if !ok {
		t.Fatalf("auth = %#v, want an entry named first-auth", cfg.Auth)
	}
	if first.Type != "api_key" {
		t.Errorf("first-auth type = %q, want api_key", first.Type)
	}
	if first.APIKey != "sk-first" {
		t.Errorf("first-auth api_key = %q, want the declared key", first.APIKey)
	}
}

func TestLoadProviderBaseURLSubstitution(t *testing.T) {
	writeModules(t,
		``,
		standardAuth,
		`{
  "providers": {
    "custom": {
      "adapter": "openai-compatible",
      "base_url": "${CUSTOM_ENDPOINT}",
      "auth": "first-auth",
      "models": ["model-a"]
    }
  }
}`)
	t.Setenv("CUSTOM_ENDPOINT", "https://api.custom.example.com/v1")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Providers["custom"].BaseURL; got != "https://api.custom.example.com/v1" {
		t.Errorf("base_url = %q, want env substitution", got)
	}
}

func TestLoadAuthKeySubstitution(t *testing.T) {
	writeModules(t,
		``,
		`{ "custom-auth": { "type": "api_key", "api_key": "${CUSTOM_KEY}" } }`,
		standardModels)
	t.Setenv("CUSTOM_KEY", "sk-substituted")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Auth["custom-auth"].APIKey; got != "sk-substituted" {
		t.Errorf("api_key = %q, want env substitution", got)
	}
}

func TestLoadAuthKeyWithDefaultFallsBack(t *testing.T) {
	writeModules(t,
		``,
		`{ "custom-auth": { "type": "api_key", "api_key": "${CUSTOM_KEY:-sk-fallback}" } }`,
		``)
	_ = os.Unsetenv("CUSTOM_KEY")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Auth["custom-auth"].APIKey; got != "sk-fallback" {
		t.Errorf("api_key = %q, want the placeholder default", got)
	}
}

func TestLoadUnresolvedAuthKeyStaysEmpty(t *testing.T) {
	// A missing secret makes the Provider unauthenticatable; it must not
	// fail the whole startup, so other Providers remain usable.
	writeModules(t,
		``,
		`{ "custom-auth": { "type": "api_key", "api_key": "${CUSTOM_KEY}" } }`,
		standardModels)
	_ = os.Unsetenv("CUSTOM_KEY")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v; an unresolved auth key must not fail Load", err)
	}
	if got := cfg.Auth["custom-auth"].APIKey; got != "" {
		t.Errorf("api_key = %q, want empty for an unset placeholder", got)
	}
}

func TestLoadDefaultSelectionFromSettings(t *testing.T) {
	writeModules(t,
		`{ "defaultProvider": "first", "defaultModel": "model-a" }`,
		standardAuth,
		standardModels)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := provider.Selection{Provider: "first", Model: "model-a"}
	if cfg.Selection != want {
		t.Errorf("selection = %#v, want %#v", cfg.Selection, want)
	}
}

func TestLoadDefaultSelectionSubstitution(t *testing.T) {
	writeModules(t,
		`{ "defaultProvider": "${DEFAULT_PROVIDER}", "defaultModel": "${DEFAULT_MODEL:-model-a}" }`,
		standardAuth,
		standardModels)
	t.Setenv("DEFAULT_PROVIDER", "second")
	_ = os.Unsetenv("DEFAULT_MODEL")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := provider.Selection{Provider: "second", Model: "model-a"}
	if cfg.Selection != want {
		t.Errorf("selection = %#v, want %#v", cfg.Selection, want)
	}
}

func TestLoadPartialDefaultSelectionAllowed(t *testing.T) {
	// An incomplete default Selection is surfaced by Selection resolution at
	// startup, not by Load.
	writeModules(t, `{ "defaultProvider": "first" }`, ``, ``)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Selection.Provider != "first" || cfg.Selection.Model != "" {
		t.Errorf("selection = %#v, want the partial default preserved", cfg.Selection)
	}
}

func TestLoadAuthenticationsSeparateFromProviders(t *testing.T) {
	// Providers without any auth module still load: their Authentication
	// simply does not resolve, which Selection reports — the declaration and
	// the credentials are independent modules.
	writeModules(t, ``, ``, standardModels)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("providers = %#v, want both entries without an auth module", cfg.Providers)
	}
	if cfg.Auth != nil {
		t.Errorf("auth = %#v, want none", cfg.Auth)
	}
}

// --- Issue #34/#36 acceptance, ported: Env Substitution, .env, precedence ---

func TestSectionsReplaceAsAWhole(t *testing.T) {
	// settings.json declares only the approval section. It replaces the
	// settings section as a whole: fields absent from the file do not merge
	// from any other source, and the other modules are independent sections.
	writeModules(t,
		`{ "approval": { "tools": { "edit": "destructive" } } }`,
		standardAuth,
		standardModels)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Approval.Tools["edit"]; got != "destructive" {
		t.Errorf("approval.tools.edit = %q, want destructive", got)
	}
	// No field-by-field merging: approval.defaults is absent, not half-filled.
	if cfg.Approval.Defaults != nil {
		t.Errorf("approval.defaults = %#v, want nil (absent section field)", cfg.Approval.Defaults)
	}
	if cfg.RepoMap.Exclude != nil {
		t.Errorf("repo_map.exclude = %#v, want nil (absent section)", cfg.RepoMap.Exclude)
	}
	// Built-in defaults still apply to the settings fields the file omits.
	if cfg.AgentCommits.Mode != "off" {
		t.Errorf("agent_commits.mode = %q, want built-in default off", cfg.AgentCommits.Mode)
	}
}

func TestLiteralOverridesEnvironmentPlaceholder(t *testing.T) {
	// The global settings module leaves the validation command to the
	// environment; a trusted project's literal replaces it at higher
	// precedence, pinning a value the lower source left to the environment.
	project := t.TempDir()
	writeModules(t,
		`{ "agent_commits": { "validation": "${VALIDATION_CMD}" } }`, ``, ``)
	writeProject(t, project, map[string]string{
		"settings.json": `{ "agent_commits": { "validation": "make ci" } }`,
	})
	grantTrust(t, project)
	t.Setenv("VALIDATION_CMD", "go test ./...")

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Validation != "make ci" {
		t.Errorf("validation = %q, want the higher-precedence literal to override the placeholder", cfg.AgentCommits.Validation)
	}
}

func TestLoadFromDekuHomeEnvFile(t *testing.T) {
	writeDekuHome(t, map[string]string{
		"models.json": `{
  "providers": {
    "custom": {
      "adapter": "openai-compatible",
      "base_url": "${DEKU_ENDPOINT}",
      "auth": "custom-auth",
      "models": ["model-a"]
    }
  }
}`,
		"auth.json": `{ "custom-auth": { "type": "api_key", "api_key": "${DEKU_API_KEY}" } }`,
		".env": `DEKU_ENDPOINT=https://api.envfile.example.com/v1
DEKU_API_KEY=sk-envfile-key
`,
	})

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Providers["custom"].BaseURL; got != "https://api.envfile.example.com/v1" {
		t.Errorf("base_url = %q, want substitution from the .env file", got)
	}
	if got := cfg.Auth["custom-auth"].APIKey; got != "sk-envfile-key" {
		t.Errorf("api_key = %q, want substitution from the .env file", got)
	}
}

func TestProcessEnvWinsOverDekuHomeEnv(t *testing.T) {
	writeDekuHome(t, map[string]string{
		"auth.json": `{ "custom-auth": { "type": "api_key", "api_key": "${DEKU_API_KEY}" } }`,
		".env": `DEKU_API_KEY=sk-envfile-key
`,
	})
	t.Setenv("DEKU_API_KEY", "sk-process-key")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Auth["custom-auth"].APIKey; got != "sk-process-key" {
		t.Errorf("api_key = %q, want process env to win over .env", got)
	}
}

func TestMalformedEnvFileFails(t *testing.T) {
	writeEnv(t, `DEKU_API_KEY=sk-key
this line has no equals sign
`)

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for malformed .env")
	}
	if !strings.Contains(err.Error(), ".env") {
		t.Errorf("error = %q, want it to name .env", err)
	}
}

// --- Issue #38 acceptance, ported: Project Config and Project Trust ---

// writeProject writes Project Config modules under <root>/.deku for the
// current test. An empty body skips that file, so a missing module is simply
// absent.
func writeProject(t *testing.T, root string, files map[string]string) {
	t.Helper()
	dekuDir := filepath.Join(root, ".deku")
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
}

// grantTrust writes the Deku Home trust record listing the given project
// roots. The test must have set HOME first (via writeDekuHome or writeEnv).
func grantTrust(t *testing.T, projects ...string) {
	t.Helper()
	data, err := json.Marshal(trustFile{Projects: projects})
	if err != nil {
		t.Fatal(err)
	}
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME is not set; call writeDekuHome or writeEnv first")
	}
	if err := os.WriteFile(filepath.Join(home, ".deku", trustFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// projectModels is a project-scope models module declaring one Provider that
// shadows nothing in the global registry on purpose.
const projectModels = `{
  "providers": {
    "project-provider": {
      "adapter": "openai-compatible",
      "base_url": "https://api.project.example.com/v1",
      "auth": "project-auth",
      "models": ["project-model"]
    }
  }
}`

func TestProjectModelsReplaceGlobalAsAWhole(t *testing.T) {
	project := t.TempDir()
	writeModules(t, ``, standardAuth, standardModels)
	writeProject(t, project, map[string]string{"models.json": projectModels})
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("providers = %#v, want only the project registry", cfg.Providers)
	}
	entry, ok := cfg.Providers["project-provider"]
	if !ok {
		t.Fatalf("providers = %#v, want the project provider", cfg.Providers)
	}
	if entry.BaseURL != "https://api.project.example.com/v1" {
		t.Errorf("base_url = %q, want the project value", entry.BaseURL)
	}
}

func TestProjectAuthReplacesGlobalAsAWhole(t *testing.T) {
	project := t.TempDir()
	writeModules(t, ``, standardAuth, standardModels)
	writeProject(t, project, map[string]string{
		"auth.json": `{ "project-auth": { "type": "api_key", "api_key": "sk-project" } }`,
	})
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("auth = %#v, want only the project credentials", cfg.Auth)
	}
	if got := cfg.Auth["project-auth"].APIKey; got != "sk-project" {
		t.Errorf("project-auth api_key = %q, want the project value", got)
	}
}

func TestProjectSettingsReplaceGlobalDefaults(t *testing.T) {
	project := t.TempDir()
	writeModules(t,
		`{ "defaultProvider": "first", "defaultModel": "model-a" }`,
		standardAuth,
		standardModels)
	writeProject(t, project, map[string]string{
		"settings.json": `{ "defaultProvider": "project-provider", "defaultModel": "project-model" }`,
	})
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := provider.Selection{Provider: "project-provider", Model: "project-model"}
	if cfg.Selection != want {
		t.Errorf("selection = %#v, want the project defaults", cfg.Selection)
	}
}

func TestProjectModuleAbsentLeavesGlobalSection(t *testing.T) {
	project := t.TempDir()
	writeModules(t, ``, standardAuth, standardModels)
	// The project carries only a settings module; the registry stands.
	writeProject(t, project, map[string]string{
		"settings.json": `{ "agent_commits": { "mode": "on" } }`,
	})
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("providers = %#v, want the global registry when the project has no models module", cfg.Providers)
	}
	if cfg.AgentCommits.Mode != "on" {
		t.Errorf("agent_commits.mode = %q, want project value", cfg.AgentCommits.Mode)
	}
}

func TestUntrustedProjectConfigIgnored(t *testing.T) {
	project := t.TempDir()
	writeModules(t, ``, standardAuth, standardModels)
	writeProject(t, project, map[string]string{"models.json": projectModels})
	// No trust record: the project is untrusted.

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("providers = %#v, want the global registry; untrusted project must be ignored", cfg.Providers)
	}
}

func TestUntrustedProjectConfigNotLoaded(t *testing.T) {
	project := t.TempDir()
	writeModules(t, ``, standardAuth, standardModels)
	// A malformed project module must not fail the load: an untrusted
	// project's files are never read.
	writeProject(t, project, map[string]string{"models.json": `{ not valid json`})

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v; untrusted project files must not be read", err)
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("providers = %#v, want the global registry", cfg.Providers)
	}
}

func TestMissingTrustRecordTrustsNothing(t *testing.T) {
	project := t.TempDir()
	writeModules(t, ``, standardAuth, standardModels)
	writeProject(t, project, map[string]string{"models.json": projectModels})

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("providers = %#v, want the global registry; absent trust record must trust nothing", cfg.Providers)
	}
}

func TestTrustRequiresExactProjectRoot(t *testing.T) {
	project := t.TempDir()
	writeModules(t, ``, standardAuth, standardModels)
	writeProject(t, project, map[string]string{"models.json": projectModels})
	// A sibling path and a nested path are not the project root.
	grantTrust(t, project+"-copy", filepath.Join(project, "sub"))

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("providers = %#v, want the global registry; trust must match the exact root", cfg.Providers)
	}

	// Path normalization: a cleaned variant of the same root matches.
	grantTrust(t, filepath.Join(project, "."))
	cfg, err = Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Errorf("providers = %#v, want the project registry after trust via cleaned path", cfg.Providers)
	}
}

func TestMalformedTrustRecordFails(t *testing.T) {
	project := t.TempDir()
	writeDekuHome(t, map[string]string{
		trustFileName: `{ not valid json`,
	})

	_, err := Load(project)
	if err == nil {
		t.Fatal("expected error for malformed trust record")
	}
	if !strings.Contains(err.Error(), trustFileName) {
		t.Errorf("error = %q, want it to name %s", err, trustFileName)
	}
}

func TestProjectAuthSubstitutionFromEnvFile(t *testing.T) {
	project := t.TempDir()
	writeDekuHome(t, map[string]string{
		".env": `PROJECT_KEY=sk-project-envfile
`,
	})
	writeProject(t, project, map[string]string{
		"auth.json":   `{ "project-auth": { "type": "api_key", "api_key": "${PROJECT_KEY}" } }`,
		"models.json": projectModels,
	})
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Auth["project-auth"].APIKey; got != "sk-project-envfile" {
		t.Errorf("api_key = %q, want substitution from Deku Home .env", got)
	}
}

func TestProjectScopeReportedWhenLoaded(t *testing.T) {
	project := t.TempDir()
	writeModules(t, ``, standardAuth, standardModels)
	writeProject(t, project, map[string]string{
		"settings.json": `{ "agent_commits": { "mode": "on" } }`,
	})
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Project.Root != project {
		t.Errorf("project root = %q, want %q", cfg.Project.Root, project)
	}
	if !cfg.Project.Present || !cfg.Project.Trusted || !cfg.Project.Loaded {
		t.Errorf("project scope = %+v, want present, trusted, and loaded", cfg.Project)
	}
}

func TestProjectScopeReportedWhenUntrusted(t *testing.T) {
	project := t.TempDir()
	writeModules(t, ``, standardAuth, standardModels)
	writeProject(t, project, map[string]string{
		"settings.json": `{ "agent_commits": { "mode": "on" } }`,
	})

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Project.Root != project {
		t.Errorf("project root = %q, want %q", cfg.Project.Root, project)
	}
	if !cfg.Project.Present || cfg.Project.Trusted || cfg.Project.Loaded {
		t.Errorf("project scope = %+v, want present but untrusted and not loaded", cfg.Project)
	}
}

func TestProjectScopeAbsentOutsideRepository(t *testing.T) {
	writeModules(t, ``, standardAuth, standardModels)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Project != (ProjectScope{}) {
		t.Errorf("project scope = %+v, want zero value outside a repository", cfg.Project)
	}
}

func TestProjectScopeTrustedEmptyDirectoryNotLoaded(t *testing.T) {
	project := t.TempDir()
	writeModules(t, ``, standardAuth, standardModels)
	if err := os.MkdirAll(filepath.Join(project, ".deku"), 0o755); err != nil {
		t.Fatal(err)
	}
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Project.Present || !cfg.Project.Trusted || cfg.Project.Loaded {
		t.Errorf("project scope = %+v, want present and trusted but nothing loaded", cfg.Project)
	}
}

func TestGrantTrustCreatesRecord(t *testing.T) {
	project := t.TempDir()
	writeDekuHome(t, map[string]string{})

	if err := GrantTrust(project); err != nil {
		t.Fatalf("GrantTrust() error = %v", err)
	}
	record := readTrustRecord(t)
	want := []string{filepath.Clean(project)}
	if !reflect.DeepEqual(record.Projects, want) {
		t.Errorf("trust record projects = %#v, want %#v", record.Projects, want)
	}

	// The grant takes effect: the project's config now loads.
	writeProject(t, project, map[string]string{
		"settings.json": `{ "agent_commits": { "mode": "on" } }`,
	})
	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Project.Trusted || !cfg.Project.Loaded {
		t.Errorf("project scope = %+v, want trusted and loaded after grant", cfg.Project)
	}
	if cfg.AgentCommits.Mode != "on" {
		t.Errorf("agent_commits.mode = %q, want project value after grant", cfg.AgentCommits.Mode)
	}
}

func TestGrantTrustAppendsPreservingExistingEntries(t *testing.T) {
	project := t.TempDir()
	other := filepath.Join(t.TempDir(), "other")
	writeDekuHome(t, map[string]string{
		trustFileName: `{ "projects": ["` + other + `"] }`,
	})

	if err := GrantTrust(project); err != nil {
		t.Fatalf("GrantTrust() error = %v", err)
	}
	record := readTrustRecord(t)
	want := []string{filepath.Clean(other), filepath.Clean(project)}
	if !reflect.DeepEqual(record.Projects, want) {
		t.Errorf("trust record projects = %#v, want %#v", record.Projects, want)
	}
}

func TestGrantTrustIdempotent(t *testing.T) {
	project := t.TempDir()
	writeDekuHome(t, map[string]string{
		trustFileName: `{ "projects": ["` + filepath.Clean(project) + `"] }`,
	})

	if err := GrantTrust(project); err != nil {
		t.Fatalf("GrantTrust() error = %v", err)
	}
	record := readTrustRecord(t)
	if len(record.Projects) != 1 || record.Projects[0] != filepath.Clean(project) {
		t.Errorf("trust record projects = %#v, want a single entry for the project", record.Projects)
	}
}

func TestGrantTrustMalformedRecordFails(t *testing.T) {
	writeDekuHome(t, map[string]string{
		trustFileName: `{ not valid json`,
	})

	if err := GrantTrust(t.TempDir()); err == nil {
		t.Fatal("expected error for malformed trust record")
	}
}

// readTrustRecord reads the Deku Home trust record written by the current
// test.
func readTrustRecord(t *testing.T) trustFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".deku", trustFileName))
	if err != nil {
		t.Fatalf("read trust record: %v", err)
	}
	var record trustFile
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode trust record: %v", err)
	}
	return record
}

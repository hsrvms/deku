package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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

func TestLoadValidatesRequiredFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_ = os.Unsetenv("DEKU_PROVIDER_ENDPOINT")
	_ = os.Unsetenv("DEKU_PROVIDER_API_KEY")
	_ = os.Unsetenv("DEKU_PROVIDER_MODEL")

	cfg, err := Load("")
	if err == nil {
		t.Fatalf("expected error for missing config, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got %+v", cfg)
	}
}

func TestLoadRequiredFailureNamesTheField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "sk-test-key")
	_ = os.Unsetenv("DEKU_PROVIDER_MODEL")

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for missing model")
	}
	if !strings.Contains(err.Error(), "provider model is required") {
		t.Errorf("error = %q, want it to name the missing field", err)
	}
}

func TestLoadFromEnvVars(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "sk-test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.example.com/v1" {
		t.Errorf("endpoint = %q, want %q", cfg.Provider.Endpoint, "https://api.example.com/v1")
	}
	if cfg.Provider.APIKey != "sk-test-key" {
		t.Errorf("api_key = %q, want %q", cfg.Provider.APIKey, "sk-test-key")
	}
	if cfg.Provider.Model != "test-model" {
		t.Errorf("model = %q, want %q", cfg.Provider.Model, "test-model")
	}
}

func TestLoadFromModuleFiles(t *testing.T) {
	writeModules(t,
		``,
		`{ "api_key": "sk-file-key" }`,
		`{
  "endpoint": "https://api.file.com/v1",
  "model": "file-model"
}`)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.file.com/v1" {
		t.Errorf("endpoint = %q, want %q", cfg.Provider.Endpoint, "https://api.file.com/v1")
	}
	if cfg.Provider.APIKey != "sk-file-key" {
		t.Errorf("api_key = %q, want %q", cfg.Provider.APIKey, "sk-file-key")
	}
	if cfg.Provider.Model != "file-model" {
		t.Errorf("model = %q, want %q", cfg.Provider.Model, "file-model")
	}
}

func TestEnvVarsOverrideModuleFiles(t *testing.T) {
	writeModules(t,
		``,
		`{ "api_key": "sk-file-key" }`,
		`{
  "endpoint": "https://api.file.com/v1",
  "model": "file-model"
}`)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.env.com/v1")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Env var should win over the files.
	if cfg.Provider.Endpoint != "https://api.env.com/v1" {
		t.Errorf("endpoint = %q, want env override %q", cfg.Provider.Endpoint, "https://api.env.com/v1")
	}
	// File values should fill in unset env vars.
	if cfg.Provider.APIKey != "sk-file-key" {
		t.Errorf("api_key = %q, want file fallback %q", cfg.Provider.APIKey, "sk-file-key")
	}
}

func TestLoadMissingProviderEndpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_API_KEY", "sk-test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")
	_ = os.Unsetenv("DEKU_PROVIDER_ENDPOINT")

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestLoadMissingAPIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")
	_ = os.Unsetenv("DEKU_PROVIDER_API_KEY")

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for missing api key")
	}
}

func TestLoadMissingModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "sk-test-key")
	_ = os.Unsetenv("DEKU_PROVIDER_MODEL")

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestLoadRequiredErrorNamesTheModuleFile(t *testing.T) {
	// endpoint and model come from models.json; the missing api_key error
	// must point at auth.json, not at a generic config file.
	writeModules(t,
		``,
		``,
		`{
  "endpoint": "https://api.file.com/v1",
  "model": "file-model"
}`)
	_ = os.Unsetenv("DEKU_PROVIDER_API_KEY")

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for missing api key")
	}
	if !strings.Contains(err.Error(), "auth.json") {
		t.Errorf("error = %q, want it to name auth.json", err)
	}

	// A missing endpoint must point at models.json.
	writeModules(t, ``, `{ "api_key": "sk-file-key" }`, `{ "model": "file-model" }`)
	_ = os.Unsetenv("DEKU_PROVIDER_ENDPOINT")
	_, err = Load("")
	if err == nil {
		t.Fatal("expected error for missing endpoint")
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
}`,
		`{ "api_key": "sk-file-key" }`,
		`{
  "endpoint": "https://api.file.com/v1",
  "model": "file-model"
}`)

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
}`,
		`{ "api_key": "sk-file-key" }`,
		`{
  "endpoint": "https://api.file.com/v1",
  "model": "file-model"
}`)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.RepoMap.Exclude; !reflect.DeepEqual(got, []string{"vendor", "*.gen.go"}) {
		t.Errorf("repo_map.exclude = %#v, want [vendor *.gen.go]", got)
	}
}

func TestLoadNoConfigFileIsOk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "sk-test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error when no module files exist: %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.example.com/v1" {
		t.Errorf("endpoint = %q, want %q", cfg.Provider.Endpoint, "https://api.example.com/v1")
	}
}

func TestAgentCommitsDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "sk-test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")

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
}`,
		`{ "api_key": "sk-file-key" }`,
		`{
  "endpoint": "https://api.file.com/v1",
  "model": "file-model"
}`)

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

// --- Issue #34 acceptance: Env Substitution and Config Precedence ---

func TestPlaceholderResolvesFromEnvironment(t *testing.T) {
	writeModules(t,
		``,
		`{ "api_key": "${API_KEY}" }`,
		`{
  "endpoint": "${ENDPOINT}",
  "model": "${MODEL}"
}`)
	t.Setenv("ENDPOINT", "https://api.example.com/v1")
	t.Setenv("API_KEY", "sk-placeholder-key")
	t.Setenv("MODEL", "placeholder-model")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.example.com/v1" {
		t.Errorf("endpoint = %q, want env substitution", cfg.Provider.Endpoint)
	}
	if cfg.Provider.Model != "placeholder-model" {
		t.Errorf("model = %q, want env substitution", cfg.Provider.Model)
	}
	if cfg.Provider.APIKey != "sk-placeholder-key" {
		t.Errorf("api_key = %q, want env substitution", cfg.Provider.APIKey)
	}
}

func TestPlaceholderWithDefaultFallsBackWhenUnset(t *testing.T) {
	writeModules(t,
		``,
		`{ "api_key": "${API_KEY:-sk-default-key}" }`,
		`{
  "endpoint": "${DEKU_PROVIDER_ENDPOINT:-https://default.example.com/v1}",
  "model": "${MODEL:-default-model}"
}`)
	_ = os.Unsetenv("DEKU_PROVIDER_ENDPOINT")
	_ = os.Unsetenv("API_KEY")
	_ = os.Unsetenv("MODEL")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Provider.Endpoint != "https://default.example.com/v1" {
		t.Errorf("endpoint = %q, want fallback default", cfg.Provider.Endpoint)
	}
	if cfg.Provider.APIKey != "sk-default-key" {
		t.Errorf("api_key = %q, want fallback default", cfg.Provider.APIKey)
	}
	if cfg.Provider.Model != "default-model" {
		t.Errorf("model = %q, want fallback default", cfg.Provider.Model)
	}
}

func TestPlaceholderWithDefaultPrefersEnvWhenSet(t *testing.T) {
	writeModules(t,
		``,
		`{ "api_key": "${API_KEY:-sk-default-key}" }`,
		`{
  "endpoint": "${ENDPOINT:-https://default.example.com/v1}",
  "model": "${MODEL:-default-model}"
}`)
	t.Setenv("ENDPOINT", "https://api.example.com/v1")
	t.Setenv("API_KEY", "sk-env-key")
	t.Setenv("MODEL", "env-model")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.example.com/v1" {
		t.Errorf("endpoint = %q, want env value over fallback", cfg.Provider.Endpoint)
	}
	if cfg.Provider.APIKey != "sk-env-key" {
		t.Errorf("api_key = %q, want env value over fallback", cfg.Provider.APIKey)
	}
	if cfg.Provider.Model != "env-model" {
		t.Errorf("model = %q, want env value over fallback", cfg.Provider.Model)
	}
}

func TestUnsetPlaceholderWithoutDefaultFails(t *testing.T) {
	writeModules(t,
		``,
		`{ "api_key": "${API_KEY}" }`,
		`{
  "endpoint": "${ENDPOINT}",
  "model": "file-model"
}`)
	_ = os.Unsetenv("ENDPOINT")
	_ = os.Unsetenv("API_KEY")

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for unset placeholder without default")
	}
	if !strings.Contains(err.Error(), "ENDPOINT") {
		t.Errorf("error = %q, want it to name the unset variable", err)
	}
}

func TestLiteralOverridesEnvironmentPlaceholder(t *testing.T) {
	// models.json leaves "model" to the environment via ${SOME_MODEL}.
	// The environment-as-source layer (DEKU_PROVIDER_MODEL) pins a literal at
	// higher precedence, so the literal overrides the placeholder.
	writeModules(t,
		``,
		`{ "api_key": "sk-file-key" }`,
		`{
  "endpoint": "https://api.example.com/v1",
  "model": "${SOME_MODEL}"
}`)
	t.Setenv("SOME_MODEL", "placeholder-model")
	t.Setenv("DEKU_PROVIDER_MODEL", "env-source-model")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Provider.Model != "env-source-model" {
		t.Errorf("model = %q, want env-as-source literal to override placeholder", cfg.Provider.Model)
	}
}

func TestConfigPrecedenceDefaultsGlobalEnv(t *testing.T) {
	// agent_commits.mode: default "off" < settings "ask" < env-as-source "on".
	writeModules(t,
		`{ "agent_commits": { "mode": "ask" } }`,
		`{ "api_key": "sk-file-key" }`,
		`{
  "endpoint": "https://api.file.com/v1",
  "model": "file-model"
}`)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "ask" {
		t.Errorf("mode = %q, want ask from settings over default off", cfg.AgentCommits.Mode)
	}

	t.Setenv("DEKU_AGENT_COMMITS", "on")
	cfg, err = Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "on" {
		t.Errorf("mode = %q, want on from env-as-source", cfg.AgentCommits.Mode)
	}
}

func TestReplacePerSectionGlobalOverridesDefault(t *testing.T) {
	// The validation field has no settings override, so the built-in default
	// remains; when the settings section sets it, the settings value wins.
	writeModules(t,
		`{ "agent_commits": { "validation": "make ci" } }`,
		`{ "api_key": "sk-file-key" }`,
		`{
  "endpoint": "https://api.file.com/v1",
  "model": "file-model"
}`)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Validation != "make ci" {
		t.Errorf("validation = %q, want make ci from settings", cfg.AgentCommits.Validation)
	}
}

// --- Issue #36 acceptance: modular sections and Deku Home .env ---

func TestMissingModulesAreSimplyAbsent(t *testing.T) {
	// Only settings.json exists; models and auth are absent and the required
	// values come from the environment-as-source layer.
	writeModules(t,
		`{ "agent_commits": { "mode": "ask" } }`,
		``,
		``)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "sk-test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error when models/auth modules are absent: %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.example.com/v1" {
		t.Errorf("endpoint = %q, want env value", cfg.Provider.Endpoint)
	}
	if cfg.AgentCommits.Mode != "ask" {
		t.Errorf("agent_commits.mode = %q, want ask from settings", cfg.AgentCommits.Mode)
	}
}

func TestSectionsReplaceAsAWhole(t *testing.T) {
	// settings.json declares only the approval section. It replaces the
	// settings section as a whole: fields absent from the file do not merge
	// from any other source, and the other modules are independent sections.
	writeModules(t,
		`{ "approval": { "tools": { "edit": "destructive" } } }`,
		`{ "api_key": "sk-file-key" }`,
		`{
  "endpoint": "https://api.file.com/v1",
  "model": "file-model"
}`)

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

func TestLoadFromDekuHomeEnvFile(t *testing.T) {
	writeEnv(t, `# Deku Home environment
DEKU_PROVIDER_ENDPOINT=https://api.envfile.com/v1
DEKU_PROVIDER_API_KEY=sk-envfile-key
DEKU_PROVIDER_MODEL=envfile-model
`)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.envfile.com/v1" {
		t.Errorf("endpoint = %q, want %q", cfg.Provider.Endpoint, "https://api.envfile.com/v1")
	}
	if cfg.Provider.APIKey != "sk-envfile-key" {
		t.Errorf("api_key = %q, want %q", cfg.Provider.APIKey, "sk-envfile-key")
	}
	if cfg.Provider.Model != "envfile-model" {
		t.Errorf("model = %q, want %q", cfg.Provider.Model, "envfile-model")
	}
}

func TestProcessEnvWinsOverDekuHomeEnv(t *testing.T) {
	writeEnv(t, `DEKU_PROVIDER_ENDPOINT=https://api.envfile.com/v1
DEKU_PROVIDER_API_KEY=sk-envfile-key
DEKU_PROVIDER_MODEL=envfile-model
`)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.process.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "sk-process-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "process-model")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.process.com/v1" {
		t.Errorf("endpoint = %q, want process env to win over .env", cfg.Provider.Endpoint)
	}
	if cfg.Provider.APIKey != "sk-process-key" {
		t.Errorf("api_key = %q, want process env to win over .env", cfg.Provider.APIKey)
	}
	if cfg.Provider.Model != "process-model" {
		t.Errorf("model = %q, want process env to win over .env", cfg.Provider.Model)
	}
}

func TestDekuHomeEnvWinsOverModuleFiles(t *testing.T) {
	writeDekuHome(t, map[string]string{
		"auth.json": `{ "api_key": "sk-file-key" }`,
		"models.json": `{
  "endpoint": "https://api.file.com/v1",
  "model": "file-model"
}`,
		".env": `DEKU_PROVIDER_API_KEY=sk-envfile-key
`,
	})

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider.APIKey != "sk-envfile-key" {
		t.Errorf("api_key = %q, want .env to win over auth.json", cfg.Provider.APIKey)
	}
	// Values absent from .env still come from the module files.
	if cfg.Provider.Endpoint != "https://api.file.com/v1" {
		t.Errorf("endpoint = %q, want fallback to models.json", cfg.Provider.Endpoint)
	}
}

func TestEnvFileFeedsSubstitution(t *testing.T) {
	// auth.json references a variable defined only in the Deku Home .env.
	writeDekuHome(t, map[string]string{
		"auth.json": `{ "api_key": "${DEKU_API_KEY}" }`,
		"models.json": `{
  "endpoint": "${DEKU_ENDPOINT:-https://fallback.example.com/v1}",
  "model": "envfile-model"
}`,
		".env": `DEKU_API_KEY=sk-substituted-key
DEKU_ENDPOINT=https://api.substituted.com/v1
`,
	})

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider.APIKey != "sk-substituted-key" {
		t.Errorf("api_key = %q, want substitution from .env", cfg.Provider.APIKey)
	}
	if cfg.Provider.Endpoint != "https://api.substituted.com/v1" {
		t.Errorf("endpoint = %q, want substitution from .env", cfg.Provider.Endpoint)
	}
}

func TestProcessEnvWinsOverEnvFileInSubstitution(t *testing.T) {
	writeDekuHome(t, map[string]string{
		"auth.json":   `{ "api_key": "${DEKU_API_KEY}" }`,
		"models.json": `{ "endpoint": "https://api.file.com/v1", "model": "file-model" }`,
		".env": `DEKU_API_KEY=sk-envfile-key
`,
	})
	t.Setenv("DEKU_API_KEY", "sk-process-key")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider.APIKey != "sk-process-key" {
		t.Errorf("api_key = %q, want process env to win in substitution", cfg.Provider.APIKey)
	}
}

func TestMalformedEnvFileFails(t *testing.T) {
	writeEnv(t, `DEKU_PROVIDER_ENDPOINT=https://api.example.com/v1
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

func TestSettingsPlaceholderResolvesFromEnvFile(t *testing.T) {
	writeDekuHome(t, map[string]string{
		"settings.json": `{ "agent_commits": { "validation": "${VALIDATION_CMD}" } }`,
		"auth.json":     `{ "api_key": "sk-file-key" }`,
		"models.json":   `{ "endpoint": "https://api.file.com/v1", "model": "file-model" }`,
		".env": `VALIDATION_CMD=make ci
`,
	})

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AgentCommits.Validation != "make ci" {
		t.Errorf("validation = %q, want substitution from .env in settings", cfg.AgentCommits.Validation)
	}
}

// --- Issue #38 acceptance: Project Config and Project Trust ---

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

func TestProjectConfigOverridesDekuHome(t *testing.T) {
	project := t.TempDir()
	writeModules(t,
		`{ "agent_commits": { "mode": "ask" } }`,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
	writeProject(t, project, map[string]string{
		"settings.json": `{ "agent_commits": { "mode": "on" } }`,
		"models.json": `{
  "endpoint": "https://api.project.com/v1",
  "model": "project-model"
}`,
	})
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "on" {
		t.Errorf("agent_commits.mode = %q, want project on over global ask", cfg.AgentCommits.Mode)
	}
	if cfg.Provider.Endpoint != "https://api.project.com/v1" {
		t.Errorf("endpoint = %q, want project value", cfg.Provider.Endpoint)
	}
	if cfg.Provider.Model != "project-model" {
		t.Errorf("model = %q, want project value", cfg.Provider.Model)
	}
	// The project has no auth module, so the auth section still comes from
	// Deku Home.
	if cfg.Provider.APIKey != "sk-global-key" {
		t.Errorf("api_key = %q, want global fallback", cfg.Provider.APIKey)
	}
}

func TestProjectConfigReplacesGlobalSectionAsAWhole(t *testing.T) {
	project := t.TempDir()
	writeModules(t,
		`{ "agent_commits": { "mode": "ask", "validation": "make ci" } }`,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
	// The project settings section omits validation: replace-per-section
	// means it falls back to the built-in default, not to the Deku Home
	// value.
	writeProject(t, project, map[string]string{
		"settings.json": `{ "agent_commits": { "mode": "on" } }`,
	})
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "on" {
		t.Errorf("agent_commits.mode = %q, want project on", cfg.AgentCommits.Mode)
	}
	if cfg.AgentCommits.Validation != "go test ./..." {
		t.Errorf("agent_commits.validation = %q, want built-in default, not Deku Home value", cfg.AgentCommits.Validation)
	}
}

func TestProjectModuleAbsentLeavesGlobalSection(t *testing.T) {
	project := t.TempDir()
	writeModules(t,
		`{ "agent_commits": { "mode": "ask" } }`,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
	// The project carries only a models module; the settings section stands.
	writeProject(t, project, map[string]string{
		"models.json": `{
  "endpoint": "https://api.project.com/v1",
  "model": "project-model"
}`,
	})
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "ask" {
		t.Errorf("agent_commits.mode = %q, want global ask when project has no settings module", cfg.AgentCommits.Mode)
	}
	if cfg.Provider.Endpoint != "https://api.project.com/v1" {
		t.Errorf("endpoint = %q, want project value", cfg.Provider.Endpoint)
	}
}

func TestUntrustedProjectConfigIgnored(t *testing.T) {
	project := t.TempDir()
	writeModules(t,
		`{ "agent_commits": { "mode": "ask" } }`,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
	writeProject(t, project, map[string]string{
		"settings.json": `{ "agent_commits": { "mode": "on" } }`,
	})
	// No trust record: the project is untrusted.

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "ask" {
		t.Errorf("agent_commits.mode = %q, want global ask; untrusted project must be ignored", cfg.AgentCommits.Mode)
	}
	if cfg.Provider.Endpoint != "https://api.global.com/v1" {
		t.Errorf("endpoint = %q, want global value", cfg.Provider.Endpoint)
	}
}

func TestUntrustedProjectConfigNotLoaded(t *testing.T) {
	project := t.TempDir()
	writeModules(t,
		``,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
	// A malformed project module must not fail the load: an untrusted
	// project's files are never read.
	writeProject(t, project, map[string]string{
		"settings.json": `{ not valid json`,
	})

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v; untrusted project files must not be read", err)
	}
	if cfg.Provider.Endpoint != "https://api.global.com/v1" {
		t.Errorf("endpoint = %q, want global value", cfg.Provider.Endpoint)
	}
}

func TestMissingTrustRecordTrustsNothing(t *testing.T) {
	project := t.TempDir()
	writeModules(t,
		`{ "agent_commits": { "mode": "ask" } }`,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
	writeProject(t, project, map[string]string{
		"settings.json": `{ "agent_commits": { "mode": "on" } }`,
	})

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "ask" {
		t.Errorf("agent_commits.mode = %q, want global ask; absent trust record must trust nothing", cfg.AgentCommits.Mode)
	}
}

func TestTrustRequiresExactProjectRoot(t *testing.T) {
	project := t.TempDir()
	writeModules(t,
		`{ "agent_commits": { "mode": "ask" } }`,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
	writeProject(t, project, map[string]string{
		"settings.json": `{ "agent_commits": { "mode": "on" } }`,
	})
	// A sibling path and a nested path are not the project root.
	grantTrust(t, project+"-copy", filepath.Join(project, "sub"))

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "ask" {
		t.Errorf("agent_commits.mode = %q, want global ask; trust must match the exact root", cfg.AgentCommits.Mode)
	}

	// Path normalization: a cleaned variant of the same root matches.
	grantTrust(t, filepath.Join(project, "."))
	cfg, err = Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "on" {
		t.Errorf("agent_commits.mode = %q, want on after trust via cleaned path", cfg.AgentCommits.Mode)
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

func TestProjectConfigEnvSubstitution(t *testing.T) {
	project := t.TempDir()
	writeDekuHome(t, map[string]string{
		"models.json": `{ "endpoint": "https://api.global.com/v1", "model": "global-model" }`,
		".env": `PROJECT_KEY=sk-project-envfile
`,
	})
	writeProject(t, project, map[string]string{
		"auth.json": `{ "api_key": "${PROJECT_KEY}" }`,
		"models.json": `{
  "endpoint": "${PROJECT_ENDPOINT:-https://fallback.project.com/v1}",
  "model": "project-model"
}`,
	})
	grantTrust(t, project)
	t.Setenv("PROJECT_ENDPOINT", "https://api.project.com/v1")

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Provider.APIKey != "sk-project-envfile" {
		t.Errorf("api_key = %q, want substitution from Deku Home .env", cfg.Provider.APIKey)
	}
	if cfg.Provider.Endpoint != "https://api.project.com/v1" {
		t.Errorf("endpoint = %q, want substitution from process environment", cfg.Provider.Endpoint)
	}
}

func TestProjectModulesSupplyRequiredValues(t *testing.T) {
	project := t.TempDir()
	writeDekuHome(t, map[string]string{})
	writeProject(t, project, map[string]string{
		"auth.json":   `{ "api_key": "sk-project-key" }`,
		"models.json": `{ "endpoint": "https://api.project.com/v1", "model": "project-model" }`,
	})
	grantTrust(t, project)

	cfg, err := Load(project)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Provider.APIKey != "sk-project-key" {
		t.Errorf("api_key = %q, want project value", cfg.Provider.APIKey)
	}

	// The same repository untrusted supplies nothing: required values are
	// missing and Load fails fast.
	other := t.TempDir()
	writeProject(t, other, map[string]string{
		"auth.json":   `{ "api_key": "sk-project-key" }`,
		"models.json": `{ "endpoint": "https://api.project.com/v1", "model": "project-model" }`,
	})
	if _, err := Load(other); err == nil {
		t.Fatal("expected error when an untrusted project cannot supply required values")
	}
}

func TestProjectRootMissingDirectoryIgnored(t *testing.T) {
	writeModules(t,
		``,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
	missing := filepath.Join(t.TempDir(), "nope")
	grantTrust(t, missing)

	cfg, err := Load(missing)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.global.com/v1" {
		t.Errorf("endpoint = %q, want global value for missing project root", cfg.Provider.Endpoint)
	}
}

func TestProjectScopeReportedWhenLoaded(t *testing.T) {
	project := t.TempDir()
	writeModules(t,
		`{ "agent_commits": { "mode": "ask" } }`,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
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
	writeModules(t,
		`{ "agent_commits": { "mode": "ask" } }`,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
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
	writeModules(t,
		``,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)

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
	writeModules(t,
		`{ "agent_commits": { "mode": "ask" } }`,
		`{ "api_key": "sk-global-key" }`,
		`{
  "endpoint": "https://api.global.com/v1",
  "model": "global-model"
}`)
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
	writeDekuHome(t, map[string]string{
		"auth.json":   `{ "api_key": "sk-global-key" }`,
		"models.json": `{ "endpoint": "https://api.global.com/v1", "model": "global-model" }`,
	})

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

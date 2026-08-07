package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeGlobal writes a Deku Home config.json for the current test, pointing
// $HOME at a fresh temporary directory that contains it.
func writeGlobal(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dekuDir := filepath.Join(home, ".deku")
	if err := os.MkdirAll(dekuDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dekuDir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadValidatesRequiredFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_ = os.Unsetenv("DEKU_PROVIDER_ENDPOINT")
	_ = os.Unsetenv("DEKU_PROVIDER_API_KEY")
	_ = os.Unsetenv("DEKU_PROVIDER_MODEL")

	cfg, err := Load()
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

	_, err := Load()
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

	cfg, err := Load()
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

func TestLoadFromConfigFile(t *testing.T) {
	writeGlobal(t, `{
  "provider": {
    "endpoint": "https://api.file.com/v1",
    "api_key": "sk-file-key",
    "model": "file-model"
  }
}`)

	cfg, err := Load()
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

func TestEnvVarsOverrideConfigFile(t *testing.T) {
	writeGlobal(t, `{
  "provider": {
    "endpoint": "https://api.file.com/v1",
    "api_key": "sk-file-key",
    "model": "file-model"
  }
}`)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.env.com/v1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Env var should win over file.
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

	_, err := Load()
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

	_, err := Load()
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

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestLoadApprovalOverridesFromConfigFile(t *testing.T) {
	writeGlobal(t, `{
  "provider": {
    "endpoint": "https://api.file.com/v1",
    "api_key": "sk-file-key",
    "model": "file-model"
  },
  "approval": {
    "tools": { "edit": "destructive" },
    "defaults": { "read": "prompt", "write": "auto" }
  }
}`)

	cfg, err := Load()
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

func TestLoadRepoMapExcludeFromConfigFile(t *testing.T) {
	writeGlobal(t, `{
  "provider": {
    "endpoint": "https://api.file.com/v1",
    "api_key": "sk-file-key",
    "model": "file-model"
  },
  "repo_map": {
    "exclude": ["vendor", "*.gen.go"]
  }
}`)

	cfg, err := Load()
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

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error when config file is absent: %v", err)
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

	cfg, err := Load()
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

func TestAgentCommitsFromConfigFileAndEnv(t *testing.T) {
	writeGlobal(t, `{
  "provider": {
    "endpoint": "https://api.file.com/v1",
    "api_key": "sk-file-key",
    "model": "file-model"
  },
  "agent_commits": {
    "mode": "ask",
    "validation": "make test"
  }
}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "ask" {
		t.Errorf("agent_commits.mode = %q, want ask from file", cfg.AgentCommits.Mode)
	}
	if cfg.AgentCommits.Validation != "make test" {
		t.Errorf("agent_commits.validation = %q, want make test from file", cfg.AgentCommits.Validation)
	}

	// Environment variable overrides the file mode.
	t.Setenv("DEKU_AGENT_COMMITS", "on")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "on" {
		t.Errorf("agent_commits.mode = %q, want on from env override", cfg.AgentCommits.Mode)
	}
}

func TestLoadInvalidJSONFails(t *testing.T) {
	writeGlobal(t, `{ not valid json`)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for malformed config.json")
	}
	if !strings.Contains(err.Error(), "config.json") {
		t.Errorf("error = %q, want it to name config.json", err)
	}
}

// --- Issue #34 acceptance: Env Substitution and Config Precedence ---

func TestPlaceholderResolvesFromEnvironment(t *testing.T) {
	writeGlobal(t, `{
  "provider": {
    "endpoint": "${ENDPOINT}",
    "api_key": "${API_KEY}",
    "model": "${MODEL}"
  }
}`)
	t.Setenv("ENDPOINT", "https://api.example.com/v1")
	t.Setenv("API_KEY", "sk-placeholder-key")
	t.Setenv("MODEL", "placeholder-model")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.example.com/v1" {
		t.Errorf("endpoint = %q, want env substitution", cfg.Provider.Endpoint)
	}
	if cfg.Provider.Model != "placeholder-model" {
		t.Errorf("model = %q, want env substitution", cfg.Provider.Model)
	}
}

func TestPlaceholderWithDefaultFallsBackWhenUnset(t *testing.T) {
	writeGlobal(t, `{
  "provider": {
    "endpoint": "${DEKU_PROVIDER_ENDPOINT:-https://default.example.com/v1}",
    "api_key": "${API_KEY:-sk-default-key}",
    "model": "${MODEL:-default-model}"
  }
}`)
	_ = os.Unsetenv("DEKU_PROVIDER_ENDPOINT")
	_ = os.Unsetenv("API_KEY")
	_ = os.Unsetenv("MODEL")

	cfg, err := Load()
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
	writeGlobal(t, `{
  "provider": {
    "endpoint": "${ENDPOINT:-https://default.example.com/v1}",
    "api_key": "${API_KEY:-sk-default-key}",
    "model": "${MODEL:-default-model}"
  }
}`)
	t.Setenv("ENDPOINT", "https://api.example.com/v1")
	t.Setenv("API_KEY", "sk-env-key")
	t.Setenv("MODEL", "env-model")

	cfg, err := Load()
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
	writeGlobal(t, `{
  "provider": {
    "endpoint": "${ENDPOINT}",
    "api_key": "${API_KEY}",
    "model": "file-model"
  }
}`)
	_ = os.Unsetenv("ENDPOINT")
	_ = os.Unsetenv("API_KEY")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unset placeholder without default")
	}
	if !strings.Contains(err.Error(), "ENDPOINT") {
		t.Errorf("error = %q, want it to name the unset variable", err)
	}
}

func TestLiteralOverridesEnvironmentPlaceholder(t *testing.T) {
	// The global file leaves "model" to the environment via ${SOME_MODEL}.
	// The environment-as-source layer (DEKU_PROVIDER_MODEL) pins a literal at
	// higher precedence, so the literal overrides the placeholder.
	writeGlobal(t, `{
  "provider": {
    "endpoint": "https://api.example.com/v1",
    "api_key": "sk-file-key",
    "model": "${SOME_MODEL}"
  }
}`)
	t.Setenv("SOME_MODEL", "placeholder-model")
	t.Setenv("DEKU_PROVIDER_MODEL", "env-source-model")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Provider.Model != "env-source-model" {
		t.Errorf("model = %q, want env-as-source literal to override placeholder", cfg.Provider.Model)
	}
}

func TestConfigPrecedenceDefaultsGlobalEnv(t *testing.T) {
	// agent_commits.mode: default "off" < global "ask" < env-as-source "on".
	writeGlobal(t, `{
  "provider": {
    "endpoint": "https://api.example.com/v1",
    "api_key": "sk-file-key",
    "model": "file-model"
  },
  "agent_commits": { "mode": "ask" }
}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "ask" {
		t.Errorf("mode = %q, want ask from global over default off", cfg.AgentCommits.Mode)
	}

	t.Setenv("DEKU_AGENT_COMMITS", "on")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Mode != "on" {
		t.Errorf("mode = %q, want on from env-as-source", cfg.AgentCommits.Mode)
	}
}

func TestReplacePerSectionGlobalOverridesDefault(t *testing.T) {
	// The validation field has no global override, so the built-in default
	// remains; when the global section sets it, the global value wins.
	writeGlobal(t, `{
  "provider": {
    "endpoint": "https://api.example.com/v1",
    "api_key": "sk-file-key",
    "model": "file-model"
  },
  "agent_commits": { "validation": "make ci" }
}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentCommits.Validation != "make ci" {
		t.Errorf("validation = %q, want make ci from global", cfg.AgentCommits.Validation)
	}
}

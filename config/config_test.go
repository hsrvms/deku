package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidatesRequiredFields(t *testing.T) {
	// Missing all fields should fail.
	cfg, err := Load()
	if err == nil {
		t.Fatalf("expected error for missing config, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got %+v", cfg)
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
	home := t.TempDir()
	t.Setenv("HOME", home)

	dekuDir := filepath.Join(home, ".deku")
	if err := os.MkdirAll(dekuDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
provider:
  endpoint: "https://api.file.com/v1"
  api_key: "sk-file-key"
  model: "file-model"
`
	if err := os.WriteFile(filepath.Join(dekuDir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.env.com/v1")

	dekuDir := filepath.Join(home, ".deku")
	if err := os.MkdirAll(dekuDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
provider:
  endpoint: "https://api.file.com/v1"
  api_key: "sk-file-key"
  model: "file-model"
`
	if err := os.WriteFile(filepath.Join(dekuDir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

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

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing model")
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

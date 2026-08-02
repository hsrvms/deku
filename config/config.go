// Package config loads configuration from environment variables and
// ~/.deku/config.yaml, and validates required fields at startup.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for Deku.
type Config struct {
	Provider ProviderConfig
}

// ProviderConfig holds the OpenAI-compatible provider configuration.
type ProviderConfig struct {
	Endpoint string
	APIKey   string
	Model    string
}

// fileConfig mirrors the structure of ~/.deku/config.yaml.
type fileConfig struct {
	Provider struct {
		Endpoint string `yaml:"endpoint"`
		APIKey   string `yaml:"api_key"`
		Model    string `yaml:"model"`
	} `yaml:"provider"`
}

// Load reads configuration from ~/.deku/config.yaml and environment variables.
// Environment variables take precedence over the config file.
// Returns an error if any required field is missing or invalid.
func Load() (*Config, error) {
	cfg := &Config{}

	// Load from config file first.
	if fc, err := loadFile(); err == nil {
		cfg.Provider.Endpoint = fc.Provider.Endpoint
		cfg.Provider.APIKey = fc.Provider.APIKey
		cfg.Provider.Model = fc.Provider.Model
	}

	// Override with environment variables.
	if v := os.Getenv("DEKU_PROVIDER_ENDPOINT"); v != "" {
		cfg.Provider.Endpoint = v
	}
	if v := os.Getenv("DEKU_PROVIDER_API_KEY"); v != "" {
		cfg.Provider.APIKey = v
	}
	if v := os.Getenv("DEKU_PROVIDER_MODEL"); v != "" {
		cfg.Provider.Model = v
	}

	// Validate required fields.
	if cfg.Provider.Endpoint == "" {
		return nil, fmt.Errorf("provider endpoint is required: set DEKU_PROVIDER_ENDPOINT or provider.endpoint in ~/.deku/config.yaml")
	}
	if cfg.Provider.APIKey == "" {
		return nil, fmt.Errorf("provider API key is required: set DEKU_PROVIDER_API_KEY or provider.api_key in ~/.deku/config.yaml")
	}
	if cfg.Provider.Model == "" {
		return nil, fmt.Errorf("provider model is required: set DEKU_PROVIDER_MODEL or provider.model in ~/.deku/config.yaml")
	}

	return cfg, nil
}

func loadFile() (*fileConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".deku", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fc fileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, err
	}
	return &fc, nil
}
// Package config loads configuration from JSON files under the Deku Home
// directory and environment variables, applying Config Precedence
// (defaults < global < environment-as-source) and Env Substitution
// (${VAR} / ${VAR:-default}) to every value, and validates required fields
// at startup.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds all configuration for Deku.
type Config struct {
	Provider     ProviderConfig
	Approval     ApprovalConfig
	RepoMap      RepoMapConfig
	AgentCommits AgentCommitsConfig
}

// ProviderConfig holds the OpenAI-compatible provider configuration.
type ProviderConfig struct {
	Endpoint string
	APIKey   string
	Model    string
}

// ApprovalConfig holds Approval classification overrides. Tools maps a tool
// name to a tier override (read, write, or destructive). Defaults maps a tier
// to its enforcement action (auto or prompt).
type ApprovalConfig struct {
	Tools    map[string]string
	Defaults map[string]string
}

// RepoMapConfig holds Repository Map configuration. Exclude lists
// gitignore-style glob patterns applied in addition to any .gitignore files
// when building the map on every Step.
type RepoMapConfig struct {
	Exclude []string
}

// AgentCommitsConfig controls Git safety. Mode is off, ask, or on; Validation
// is the command run after a completed Turn before an Agent Commit is created.
type AgentCommitsConfig struct {
	Mode       string
	Validation string
}

// fileConfig mirrors the structure of ~/.deku/config.json.
type fileConfig struct {
	Provider struct {
		Endpoint string `json:"endpoint"`
		APIKey   string `json:"api_key"`
		Model    string `json:"model"`
	} `json:"provider"`
	Approval struct {
		Tools    map[string]string `json:"tools"`
		Defaults map[string]string `json:"defaults"`
	} `json:"approval"`
	RepoMap struct {
		Exclude []string `json:"exclude"`
	} `json:"repo_map"`
	AgentCommits struct {
		Mode       string `json:"mode"`
		Validation string `json:"validation"`
	} `json:"agent_commits"`
}

// These are the real process environment variables that form the
// environment-as-source layer, the highest Config Precedence source.
const (
	envKeyEndpoint      = "DEKU_PROVIDER_ENDPOINT"
	envKeyAPIKey        = "DEKU_PROVIDER_API_KEY"
	envKeyModel         = "DEKU_PROVIDER_MODEL"
	envKeyAgentCommMode = "DEKU_AGENT_COMMITS"
)

// Load reads configuration from the Deku Home JSON file and the process
// environment, resolving every value in Config Precedence order: built-in
// defaults, then the Deku Home global source, then the environment as the
// highest-precedence source. Values from the global source may be literals or
// Env Substitution placeholders (${VAR} / ${VAR:-default}); a literal value
// overrides an environment placeholder.
//
// Returns an error when a required value is missing or a placeholder
// references an unset variable with no default.
func Load() (*Config, error) {
	fc, err := loadFile()
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	cfg.AgentCommits.Mode = resolveString("off", src(fc, func(f *fileConfig) string { return f.AgentCommits.Mode }), os.Getenv(envKeyAgentCommMode))
	cfg.AgentCommits.Validation = resolveString("go test ./...", src(fc, func(f *fileConfig) string { return f.AgentCommits.Validation }), "")

	if fc != nil {
		cfg.Approval.Tools = fc.Approval.Tools
		cfg.Approval.Defaults = fc.Approval.Defaults
		cfg.RepoMap.Exclude = fc.RepoMap.Exclude
	}

	var errs []string
	cfg.Provider.Endpoint, errs = resolveRequired("provider endpoint", "DEKU_PROVIDER_ENDPOINT", src(fc, func(f *fileConfig) string { return f.Provider.Endpoint }), os.Getenv(envKeyEndpoint), errs)
	cfg.Provider.APIKey, errs = resolveRequired("provider API key", "DEKU_PROVIDER_API_KEY", src(fc, func(f *fileConfig) string { return f.Provider.APIKey }), os.Getenv(envKeyAPIKey), errs)
	cfg.Provider.Model, errs = resolveRequired("provider model", "DEKU_PROVIDER_MODEL", src(fc, func(f *fileConfig) string { return f.Provider.Model }), os.Getenv(envKeyModel), errs)

	if len(errs) > 0 {
		return nil, fmt.Errorf("configuration is incomplete: %s", strings.Join(errs, "; "))
	}
	return cfg, nil
}

// src returns the global-source value for a field, or "" when no config file
// (or field) is present.
func src(fc *fileConfig, pick func(*fileConfig) string) string {
	if fc == nil {
		return ""
	}
	return pick(fc)
}

// resolveString applies Config Precedence and Env Substitution to an optional
// string field. def is the built-in default; file is the global-source value;
// env is the environment-as-source value.
func resolveString(def, file, env string) string {
	// The environment-as-source layer is highest and holds a literal.
	if env != "" {
		return env
	}
	// Global layer, with Env Substitution. On an unresolvable placeholder the
	// literal default overrides it.
	if file != "" {
		if resolved, err := expand(file); err == nil && resolved != "" {
			return resolved
		}
	}
	return def
}

// resolveRequired applies Config Precedence and Env Substitution to a required
// string field, appending a clear error to errs when no source yields a value.
func resolveRequired(name, envName, file, env string, errs []string) (string, []string) {
	if env != "" {
		return env, errs
	}
	if file == "" {
		errs = append(errs, fmt.Sprintf("%s is required: set %s or the %q field in ~/.deku/config.json", name, envName, name))
		return "", errs
	}
	resolved, err := expand(file)
	if err != nil {
		errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		return "", errs
	}
	if resolved == "" {
		errs = append(errs, fmt.Sprintf("%s resolved to an empty value", name))
		return "", errs
	}
	return resolved, errs
}

// expand resolves Env Substitution placeholders in s: ${VAR} is replaced with
// the value of the environment variable VAR, and ${VAR:-default} supplies a
// fallback when VAR is unset or empty. A value with no placeholder is returned
// unchanged. An error is returned when ${VAR} references an unset variable.
func expand(s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	var sb strings.Builder
	for {
		start := strings.Index(s, "${")
		if start < 0 {
			sb.WriteString(s)
			return sb.String(), nil
		}
		sb.WriteString(s[:start])
		rest := s[start+2:]
		end := strings.Index(rest, "}")
		if end < 0 {
			return "", fmt.Errorf("unterminated environment placeholder in %q", s)
		}
		body := rest[:end]
		name, def, hasDef := strings.Cut(body, ":-")
		if name == "" {
			return "", fmt.Errorf("empty environment placeholder in %q", s)
		}
		if hasDef {
			if v, ok := os.LookupEnv(name); ok && v != "" {
				sb.WriteString(v)
			} else {
				sb.WriteString(def)
			}
		} else {
			v, ok := os.LookupEnv(name)
			if !ok {
				return "", fmt.Errorf("environment variable %q is not set and has no default", name)
			}
			sb.WriteString(v)
		}
		s = rest[end+1:]
	}
}

// loadFile reads and decodes ~/.deku/config.json. An absent file yields a nil
// fileConfig with no error; a malformed file yields an error.
func loadFile() (*fileConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".deku", "config.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("invalid ~/.deku/config.json: %w", err)
	}
	return &fc, nil
}

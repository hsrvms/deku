// Package config loads configuration from modular JSON files under the Deku
// Home directory, a Deku Home .env file, and the process environment,
// applying Config Precedence (defaults < Deku Home modules <
// environment-as-source) and Env Substitution (${VAR} / ${VAR:-default}) to
// every value, and validating required fields at startup.
//
// Configuration is split by risk into three optional modules: settings.json
// (behavior), auth.json (credentials), and models.json (the non-secret
// Provider declaration). A missing module is simply absent. The Deku Home
// .env file is auto-loaded as a source of environment values for secrets and
// endpoints; the real process environment always wins over it.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Module file names under the Deku Home directory.
const (
	settingsModule = "settings.json"
	authModule     = "auth.json"
	modelsModule   = "models.json"
	envFileName    = ".env"
)

// Config holds all configuration for Deku.
type Config struct {
	Provider     ProviderConfig
	Approval     ApprovalConfig
	RepoMap      RepoMapConfig
	AgentCommits AgentCommitsConfig
}

// ProviderConfig holds the OpenAI-compatible provider declaration.
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

// settingsFile mirrors the structure of ~/.deku/settings.json, the behavior
// module.
type settingsFile struct {
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

// authFile mirrors the structure of ~/.deku/auth.json, the credentials
// module.
type authFile struct {
	APIKey string `json:"api_key"`
}

// modelsFile mirrors the structure of ~/.deku/models.json, the non-secret
// Provider declaration module.
type modelsFile struct {
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
}

// These are the real process environment variables that form the
// environment-as-source layer, the highest Config Precedence source.
const (
	envKeyEndpoint      = "DEKU_PROVIDER_ENDPOINT"
	envKeyAPIKey        = "DEKU_PROVIDER_API_KEY"
	envKeyModel         = "DEKU_PROVIDER_MODEL"
	envKeyAgentCommMode = "DEKU_AGENT_COMMITS"
)

// lookup resolves an environment value. The real process environment wins;
// the Deku Home .env file is the fallback.
type lookup func(string) (string, bool)

// Load reads configuration from the Deku Home modules (settings.json,
// auth.json, models.json), the Deku Home .env file, and the process
// environment, resolving every value in Config Precedence order: built-in
// defaults, then the Deku Home modules, then the environment as the
// highest-precedence source. Values from the modules may be literals or Env
// Substitution placeholders (${VAR} / ${VAR:-default}); a literal value
// overrides an environment placeholder. Each module is a section replaced as
// a whole; a missing module is simply absent.
//
// Returns an error when a required value is missing, a placeholder references
// an unset variable with no default, or a module or .env file is malformed.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dekuHome := filepath.Join(home, ".deku")

	envFile, err := loadEnvFile(filepath.Join(dekuHome, envFileName))
	if err != nil {
		return nil, err
	}
	resolve := effectiveLookup(envFile)

	var settings *settingsFile
	if err := loadJSONFile(filepath.Join(dekuHome, settingsModule), &settings); err != nil {
		return nil, err
	}
	var auth *authFile
	if err := loadJSONFile(filepath.Join(dekuHome, authModule), &auth); err != nil {
		return nil, err
	}
	var models *modelsFile
	if err := loadJSONFile(filepath.Join(dekuHome, modelsModule), &models); err != nil {
		return nil, err
	}

	cfg := &Config{}
	if settings != nil {
		cfg.Approval.Tools = settings.Approval.Tools
		cfg.Approval.Defaults = settings.Approval.Defaults
		cfg.RepoMap.Exclude = settings.RepoMap.Exclude
	}
	cfg.AgentCommits.Mode = resolveString("off", moduleValue(settings, func(s *settingsFile) string { return s.AgentCommits.Mode }), envValue(resolve, envKeyAgentCommMode), resolve)
	cfg.AgentCommits.Validation = resolveString("go test ./...", moduleValue(settings, func(s *settingsFile) string { return s.AgentCommits.Validation }), "", resolve)

	var errs []string
	cfg.Provider.Endpoint, errs = resolveRequired("provider endpoint", envKeyEndpoint, "endpoint", modelsModule, moduleValue(models, func(m *modelsFile) string { return m.Endpoint }), envValue(resolve, envKeyEndpoint), resolve, errs)
	cfg.Provider.APIKey, errs = resolveRequired("provider API key", envKeyAPIKey, "api_key", authModule, moduleValue(auth, func(a *authFile) string { return a.APIKey }), envValue(resolve, envKeyAPIKey), resolve, errs)
	cfg.Provider.Model, errs = resolveRequired("provider model", envKeyModel, "model", modelsModule, moduleValue(models, func(m *modelsFile) string { return m.Model }), envValue(resolve, envKeyModel), resolve, errs)

	if len(errs) > 0 {
		return nil, fmt.Errorf("configuration is incomplete: %s", strings.Join(errs, "; "))
	}
	return cfg, nil
}

// effectiveLookup returns a lookup that consults the real process environment
// first and falls back to the Deku Home .env file.
func effectiveLookup(envFile map[string]string) lookup {
	return func(name string) (string, bool) {
		if v, ok := os.LookupEnv(name); ok {
			return v, true
		}
		v, ok := envFile[name]
		return v, ok
	}
}

// envValue returns the environment-as-source value for name, or "" when the
// variable is unset or empty.
func envValue(resolve lookup, name string) string {
	v, _ := resolve(name)
	return v
}

// moduleValue returns the module-source value for a field, or "" when the
// module (or field) is absent.
func moduleValue[T any](m *T, pick func(*T) string) string {
	if m == nil {
		return ""
	}
	return pick(m)
}

// resolveString applies Config Precedence and Env Substitution to an optional
// string field. def is the built-in default; file is the module-source value;
// env is the environment-as-source value.
func resolveString(def, file, env string, resolve lookup) string {
	// The environment-as-source layer is highest and holds a literal.
	if env != "" {
		return env
	}
	// Module layer, with Env Substitution. On an unresolvable placeholder the
	// literal default overrides it.
	if file != "" {
		if resolved, err := expand(file, resolve); err == nil && resolved != "" {
			return resolved
		}
	}
	return def
}

// resolveRequired applies Config Precedence and Env Substitution to a required
// string field, appending a clear error to errs when no source yields a value.
// moduleName and fieldName name where the value belongs so the error points
// at the right module file.
func resolveRequired(displayName, envName, fieldName, moduleName, file, env string, resolve lookup, errs []string) (string, []string) {
	if env != "" {
		return env, errs
	}
	if file == "" {
		errs = append(errs, fmt.Sprintf("%s is required: set %s or the %q field in ~/.deku/%s", displayName, envName, fieldName, moduleName))
		return "", errs
	}
	resolved, err := expand(file, resolve)
	if err != nil {
		errs = append(errs, fmt.Sprintf("%s: %v", displayName, err))
		return "", errs
	}
	if resolved == "" {
		errs = append(errs, fmt.Sprintf("%s resolved to an empty value", displayName))
		return "", errs
	}
	return resolved, errs
}

// expand resolves Env Substitution placeholders in s: ${VAR} is replaced with
// the value of the environment variable VAR, and ${VAR:-default} supplies a
// fallback when VAR is unset or empty. A value with no placeholder is returned
// unchanged. An error is returned when ${VAR} references an unset variable.
func expand(s string, resolve lookup) (string, error) {
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
			if v, ok := resolve(name); ok && v != "" {
				sb.WriteString(v)
			} else {
				sb.WriteString(def)
			}
		} else {
			v, ok := resolve(name)
			if !ok {
				return "", fmt.Errorf("environment variable %q is not set and has no default", name)
			}
			sb.WriteString(v)
		}
		s = rest[end+1:]
	}
}

// loadEnvFile reads and parses a NAME=VALUE environment file. Blank lines and
// lines starting with # are ignored; a malformed line yields an error. An
// absent file yields an empty map with no error.
func loadEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	env := make(map[string]string)
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid %s: line %d is not NAME=VALUE", path, i+1)
		}
		env[name] = value
	}
	return env, nil
}

// loadJSONFile decodes one Deku Home module into into. An absent file leaves
// into nil with no error; a malformed file yields an error naming the file.
func loadJSONFile(path string, into any) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("invalid %s: %w", path, err)
	}
	return nil
}

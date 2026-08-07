// Package config loads configuration from modular JSON files under the Deku
// Home directory, a Repository's Project Config, a Deku Home .env file, and
// the process environment, applying Config Precedence (defaults < Deku Home
// modules < Project Config < environment-as-source) and Env Substitution
// (${VAR} / ${VAR:-default}) to every value, and validating required fields at
// startup.
//
// Configuration is split by risk into three optional modules per scope:
// settings.json (behavior), auth.json (credentials), and models.json (the
// non-secret Provider declaration). A missing module is simply absent. Project
// Config lives in a .deku directory at the repository top level and is loaded
// only after the user grants the project Trust; an untrusted project is
// ignored entirely. The Deku Home .env file is auto-loaded as a source of
// environment values for secrets and endpoints; the real process environment
// always wins over it.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Module file names under the Deku Home directory or a Repository's .deku
// directory (Project Config).
const (
	settingsModule = "settings.json"
	authModule     = "auth.json"
	modelsModule   = "models.json"
	envFileName    = ".env"
	// trustFileName is the Deku Home record of repositories whose Project
	// Config the user has granted Trust.
	trustFileName = "trusted_projects.json"
)

// Config holds all configuration for Deku.
type Config struct {
	Provider     ProviderConfig
	Approval     ApprovalConfig
	RepoMap      RepoMapConfig
	AgentCommits AgentCommitsConfig
	// Project describes the Project Config state for the Repository Deku
	// runs in, so the CLI can report whether project-scope configuration was
	// applied or ignored.
	Project ProjectScope
}

// ProjectScope describes the Project Config state for the Repository Deku
// runs in. Root is the repository top-level directory (empty when the process
// is not inside a Git repository). Present reports whether the repository
// carries a .deku directory. Trusted reports whether the user granted the
// project Trust. Loaded reports whether at least one Project Config module
// was applied; it is always false for an untrusted project, whose files are
// never read.
type ProjectScope struct {
	Root    string
	Present bool
	Trusted bool
	Loaded  bool
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

// trustFile is the Project Trust record: the list of repository roots whose
// Project Config is loaded. Trust is granted per exact repository root.
type trustFile struct {
	Projects []string `json:"projects"`
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
// auth.json, models.json), the Deku Home .env file, the process environment,
// and — for a trusted Repository — its Project Config, resolving every value
// in Config Precedence order: built-in defaults, then the Deku Home modules,
// then Project Config, then the environment as the highest-precedence source.
// Values from the modules may be literals or Env Substitution placeholders
// (${VAR} / ${VAR:-default}); a literal value overrides an environment
// placeholder. Each module is a section replaced as a whole by the next
// higher-precedence scope that carries it; a missing module is simply absent.
//
// projectRoot is the top-level directory of the Repository ("" when the
// process is not inside a Git repository, in which case there is no project
// scope). Project Config is read only when the user has granted the project
// Trust by listing its root in the Deku Home trust record; an untrusted
// project's files are never read. cfg.Project reports the project scope
// outcome for the caller to surface.
//
// Returns an error when a required value is missing, a placeholder references
// an unset variable with no default, a module or .env file is malformed, or
// the trust record is malformed.
func Load(projectRoot string) (*Config, error) {
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

	globalSettings, err := loadModule[settingsFile](dekuHome, settingsModule)
	if err != nil {
		return nil, err
	}
	globalAuth, err := loadModule[authFile](dekuHome, authModule)
	if err != nil {
		return nil, err
	}
	globalModels, err := loadModule[modelsFile](dekuHome, modelsModule)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	// Project Config is gated by Project Trust: an untrusted project is
	// ignored entirely, so its files are never read and cannot affect the
	// session. A trusted project's modules replace the Deku Home modules of
	// the same name as a whole.
	settings := globalSettings
	auth := globalAuth
	models := globalModels
	loaded := false
	if projectRoot != "" {
		absoluteRoot, err := filepath.Abs(projectRoot)
		if err != nil {
			return nil, fmt.Errorf("resolve project root: %w", err)
		}
		projectDir := filepath.Join(absoluteRoot, ".deku")
		cfg.Project = ProjectScope{Root: absoluteRoot}
		if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
			cfg.Project.Present = true
		}
		granted, err := trusted(dekuHome, absoluteRoot)
		if err != nil {
			return nil, err
		}
		cfg.Project.Trusted = granted
		if granted {
			if projectSettings, err := loadModule[settingsFile](projectDir, settingsModule); err != nil {
				return nil, err
			} else if projectSettings != nil {
				settings = projectSettings
				loaded = true
			}
			if projectAuth, err := loadModule[authFile](projectDir, authModule); err != nil {
				return nil, err
			} else if projectAuth != nil {
				auth = projectAuth
				loaded = true
			}
			if projectModels, err := loadModule[modelsFile](projectDir, modelsModule); err != nil {
				return nil, err
			} else if projectModels != nil {
				models = projectModels
				loaded = true
			}
		}
		cfg.Project.Loaded = loaded
	}

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

// loadModule loads one optional module file named name from dir. An absent
// file yields a nil module with no error; a malformed file yields an error
// naming the file.
func loadModule[T any](dir, name string) (*T, error) {
	var m *T
	if err := loadJSONFile(filepath.Join(dir, name), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// trusted reports whether the user has granted Trust to the project at root.
// The decision is deterministic: a project is trusted only when its absolute,
// cleaned path appears in the Deku Home trust record. An absent record trusts
// nothing — an untrusted repository is never trusted by default. A malformed
// record is an error so a broken gate fails fast instead of silently changing
// the decision.
func trusted(dekuHome, projectRoot string) (bool, error) {
	record, err := loadModule[trustFile](dekuHome, trustFileName)
	if err != nil {
		return false, err
	}
	if record == nil {
		return false, nil
	}
	target := filepath.Clean(projectRoot)
	for _, project := range record.Projects {
		if filepath.Clean(project) == target {
			return true, nil
		}
	}
	return false, nil
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

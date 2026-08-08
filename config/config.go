// Package config loads configuration from modular JSON files under the Deku
// Home directory, a Repository's Project Config, a Deku Home .env file, and
// the process environment, applying Config Precedence (defaults < Deku Home
// modules < Project Config < environment-as-source) and Env Substitution
// (${VAR} / ${VAR:-default}) to every value.
//
// Configuration is split by risk into three optional modules per scope:
// settings.json (behavior and the default Selection), auth.json (named
// credentials), and models.json (the Provider Registry's non-secret
// declaration). A missing module is simply absent. Project Config lives in a
// .deku directory at the repository top level and is loaded only after the
// user grants the project Trust; an untrusted project is ignored entirely.
// The Deku Home .env file is auto-loaded as a source of environment values
// for secrets and endpoints; the real process environment always wins over
// it.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hsrvms/deku/provider"
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
	// Providers is the Provider Registry's non-secret declaration, keyed by
	// Provider name. Auth holds the named credentials the Providers reference;
	// the two are separate so secrets never travel with shared configuration.
	Providers map[string]provider.Provider
	Auth      map[string]provider.Authentication
	// Selection is the default Selection for the session, from
	// defaultProvider and defaultModel. A per-Session override recorded in
	// the Session takes precedence over it at runtime.
	Selection    provider.Selection
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

// settingsFile mirrors the structure of settings.json, the behavior module.
// DefaultProvider and DefaultModel name the default Selection for the
// session.
type settingsFile struct {
	DefaultProvider string `json:"defaultProvider"`
	DefaultModel    string `json:"defaultModel"`
	Approval        struct {
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

// authEntries mirrors the structure of auth.json, the credentials module: a
// map from Authentication name to the typed credential.
type authEntries map[string]provider.Authentication

// modelsFile mirrors the structure of models.json, the Provider Registry's
// non-secret declaration: a map from Provider name to its Adapter family,
// base URL, Authentication name, and Model Registry.
type modelsFile struct {
	Providers map[string]provider.Provider `json:"providers"`
}

// trustFile is the Project Trust record: the list of repository roots whose
// Project Config is loaded. Trust is granted per exact repository root.
type trustFile struct {
	Projects []string `json:"projects"`
}

// envKeyAgentCommMode is the environment-as-source override for the Agent
// Commits mode.
const envKeyAgentCommMode = "DEKU_AGENT_COMMITS"

// lookup resolves an environment value. The real process environment wins;
// the Deku Home .env file is the fallback.
type lookup func(string) (string, bool)

// Load reads configuration from the Deku Home modules (settings.json,
// auth.json, models.json), the Deku Home .env file, the process environment,
// and — for a trusted Repository — its Project Config, resolving every value
// in Config Precedence order: built-in defaults, then the Deku Home modules,
// then Project Config, then the environment as the highest-precedence source.
// Values from the modules may be literals or Env Substitution placeholders
// (${VAR} / ${VAR:-default}). Each module is a section replaced as a whole by
// the next higher-precedence scope that carries it; a missing module is
// simply absent.
//
// Providers and their Authentication are parsed but not validated against
// each other here: the provider Registry is responsible for structural
// validation and for reporting entries that cannot authenticate. An
// Authentication whose key placeholder does not resolve is kept with an empty
// key, so a Provider with a missing secret is excluded from Selection rather
// than failing the whole startup.
//
// projectRoot is the top-level directory of the Repository ("" when the
// process is not inside a Git repository, in which case there is no project
// scope). Project Config is read only when the user has granted the project
// Trust by listing its root in the Deku Home trust record; an untrusted
// project's files are never read. cfg.Project reports the project scope
// outcome for the caller to surface.
//
// Returns an error when a module or .env file is malformed, the trust
// record is malformed, or a required value references an unset environment
// placeholder with no default. The one deliberate exception is an
// Authentication API key: an unresolvable key leaves its Provider declared
// but unable to authenticate rather than failing startup (see resolvedAuth).
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
	globalAuth, err := loadModule[authEntries](dekuHome, authModule)
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
			if projectAuth, err := loadModule[authEntries](projectDir, authModule); err != nil {
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
		// Env Substitution applies to every value the loader consumes, not
		// only the scalar fields: a placeholder in an Approval tier, an
		// enforcement action, or a Repository Map exclusion must resolve
		// here rather than fail later with an unrelated error.
		tools, err := expandMapValues("settings.json approval.tools", settings.Approval.Tools, resolve)
		if err != nil {
			return nil, err
		}
		defaults, err := expandMapValues("settings.json approval.defaults", settings.Approval.Defaults, resolve)
		if err != nil {
			return nil, err
		}
		exclude, err := expandSliceValues("settings.json repo_map.exclude", settings.RepoMap.Exclude, resolve)
		if err != nil {
			return nil, err
		}
		cfg.Approval.Tools = tools
		cfg.Approval.Defaults = defaults
		cfg.RepoMap.Exclude = exclude
		cfg.Selection.Provider, err = resolveOptional("settings.json default_provider", settings.DefaultProvider, resolve)
		if err != nil {
			return nil, err
		}
		cfg.Selection.Model, err = resolveOptional("settings.json default_model", settings.DefaultModel, resolve)
		if err != nil {
			return nil, err
		}
	}
	mode, err := resolveRequired("settings.json agent_commits.mode", "off", moduleValue(settings, func(s *settingsFile) string { return s.AgentCommits.Mode }), envValue(resolve, envKeyAgentCommMode), resolve)
	if err != nil {
		return nil, err
	}
	cfg.AgentCommits.Mode = mode
	validation, err := resolveRequired("settings.json agent_commits.validation", "go test ./...", moduleValue(settings, func(s *settingsFile) string { return s.AgentCommits.Validation }), "", resolve)
	if err != nil {
		return nil, err
	}
	cfg.AgentCommits.Validation = validation
	providers, err := declaredProviders(models, resolve)
	if err != nil {
		return nil, err
	}
	cfg.Providers = providers
	cfg.Auth = resolvedAuth(auth, resolve)

	return cfg, nil
}

// declaredProviders resolves the Provider Registry declaration from the
// models module, filling each entry's Name from its map key and applying Env
// Substitution to the base URL and every Model name. An unresolvable
// placeholder is an error naming the variable. A nil module yields no
// Providers.
func declaredProviders(models *modelsFile, resolve lookup) (map[string]provider.Provider, error) {
	if models == nil || len(models.Providers) == 0 {
		return nil, nil
	}
	entries := make(map[string]provider.Provider, len(models.Providers))
	for name, entry := range models.Providers {
		entry.Name = name
		baseURL, err := resolveOptional("models.json "+name+" base_url", entry.BaseURL, resolve)
		if err != nil {
			return nil, err
		}
		entry.BaseURL = baseURL
		modelNames, err := expandSliceValues("models.json "+name+" models", entry.Models, resolve)
		if err != nil {
			return nil, err
		}
		entry.Models = modelNames
		entries[name] = entry
	}
	return entries, nil
}

// resolvedAuth resolves the named credentials from the auth module, applying
// Env Substitution to every API key. A nil module yields no Authentication.
func resolvedAuth(auth *authEntries, resolve lookup) map[string]provider.Authentication {
	if auth == nil || len(*auth) == 0 {
		return nil
	}
	entries := make(map[string]provider.Authentication, len(*auth))
	for name, credential := range *auth {
		credential.APIKey = resolveAuthKey(credential.APIKey, resolve)
		entries[name] = credential
	}
	return entries
}

// resolveAuthKey applies Env Substitution to an Authentication API key. An
// unresolvable placeholder yields "" rather than an error — the one
// intentional exception to fail-fast substitution, per the v0.1 development
// plan: an Authentication whose key does not resolve leaves its Provider
// declared but unable to authenticate, so a missing secret never blocks the
// other Providers. The Provider Registry reports the unauthenticatable
// entry explicitly when it is selected.
func resolveAuthKey(value string, resolve lookup) string {
	resolved, err := expandValue(value, resolve)
	if err != nil {
		return ""
	}
	return resolved
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

// GrantTrust records the user's Trust decision for the project at root in the
// Deku Home trust record, creating the record when it is absent and preserving
// existing entries. It is idempotent: an already-trusted root is left
// unchanged. A malformed existing record is an error so a broken trust record
// is never silently overwritten.
func GrantTrust(projectRoot string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dekuHome := filepath.Join(home, ".deku")
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}
	root = filepath.Clean(root)

	record, err := loadModule[trustFile](dekuHome, trustFileName)
	if err != nil {
		return err
	}
	var projects []string
	if record != nil {
		projects = record.Projects
	}
	for _, project := range projects {
		if filepath.Clean(project) == root {
			return nil
		}
	}
	data, err := json.MarshalIndent(trustFile{Projects: append(projects, root)}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dekuHome, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dekuHome, trustFileName), data, 0o600)
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

// resolveRequired applies Config Precedence and Env Substitution to a value
// that carries a built-in default. source names the module and field for
// error messages (for example "settings.json agent_commits.mode"); def is
// the built-in default; file is the module-source value; env is the
// environment-as-source value, which is literal and wins over both. An unset
// ${VAR} with no default in the module value is an error naming the
// variable: a placeholder the user wrote must never silently fall back to a
// default they did not write.
func resolveRequired(source, def, file, env string, resolve lookup) (string, error) {
	// The environment-as-source layer is highest and holds a literal.
	if env != "" {
		return env, nil
	}
	if file != "" {
		resolved, err := expand(file, resolve)
		if err != nil {
			return "", fmt.Errorf("%s: %w", source, err)
		}
		if resolved != "" {
			return resolved, nil
		}
	}
	return def, nil
}

// resolveOptional applies Env Substitution to an optional value that has no
// built-in default and no environment-as-source override. source names the
// module and field for error messages. An unset ${VAR} with no default is an
// error naming the variable: a placeholder that cannot resolve must fail
// fast here rather than surface later as a misleading "requires a base URL"
// or "no Provider or Model is selected" error.
func resolveOptional(source, value string, resolve lookup) (string, error) {
	resolved, err := expandValue(value, resolve)
	if err != nil {
		return "", fmt.Errorf("%s: %w", source, err)
	}
	return resolved, nil
}

// expandValue applies Env Substitution to one configuration value, returning
// the resolved literal or an error naming the unresolvable placeholder. It
// is the shared expansion step for every value the loader consumes; callers
// decide the error policy — resolveOptional fails fast, resolveAuthKey
// deliberately discards the error (the one documented auth-key exception).
func expandValue(value string, resolve lookup) (string, error) {
	if value == "" {
		return "", nil
	}
	return expand(value, resolve)
}

// expandMapValues applies Env Substitution to every value in m, returning an
// error naming source and the affected key when a placeholder cannot
// resolve. Keys are references, not values, and are left literal.
func expandMapValues(source string, m map[string]string, resolve lookup) (map[string]string, error) {
	if len(m) == 0 {
		return m, nil
	}
	expanded := make(map[string]string, len(m))
	for key, value := range m {
		resolved, err := expand(value, resolve)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", source, key, err)
		}
		expanded[key] = resolved
	}
	return expanded, nil
}

// expandSliceValues applies Env Substitution to every entry in values,
// returning an error naming source when a placeholder cannot resolve.
func expandSliceValues(source string, values []string, resolve lookup) ([]string, error) {
	if len(values) == 0 {
		return values, nil
	}
	expanded := make([]string, len(values))
	for index, value := range values {
		resolved, err := expand(value, resolve)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", source, index, err)
		}
		expanded[index] = resolved
	}
	return expanded, nil
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

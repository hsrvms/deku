// A Provider is a named, configured model account: it declares an Adapter
// family, an optional base URL, its Authentication by name, and the Model
// Registry it exposes. Authentication lives in a separate store keyed by name
// so secrets never travel with the non-secret Provider declaration. The
// Registry validates every entry at construction and builds the correct
// Adapter for a resolved Selection.

package provider

import (
	"fmt"
	"sort"
	"strings"
)

// Adapter family names a wire-format translator a Provider can use. v0.1
// supports only the OpenAI-compatible family; subscription families such as
// Anthropic Messages are a separate specification.
const AdapterOpenAICompatible = "openai-compatible"

// Authentication types. v0.1 supports only static API keys; OAuth is a
// separate specification.
const AuthAPIKey = "api_key"

// Authentication is the typed credential that lets a Provider be used. It is
// stored separately from the Provider declaration, addressed by name, so
// secrets never travel with shared configuration.
type Authentication struct {
	Type   string `json:"type"`
	APIKey string `json:"api_key"`
}

// Provider is a named, configured model account the Agent can run against.
// The Name is carried for callers; in configuration files it comes from the
// map key, not a field.
type Provider struct {
	Name    string   `json:"-"`
	Adapter string   `json:"adapter"`
	BaseURL string   `json:"base_url"`
	Auth    string   `json:"auth"`
	Models  []string `json:"models"`
}

// Selection pairs the Provider and Model the Agent uses for a Turn. It is
// the single Selection type in the system: configuration supplies a default,
// the Session records per-Session overrides in its transcript (the JSON form
// is part of the Session wire format and stable across resumes), and the
// Agent applies the active one.
type Selection struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// IsZero reports whether no Selection has been made.
func (s Selection) IsZero() bool {
	return s.Provider == "" && s.Model == ""
}

// Registry holds validated Provider entries and builds their Adapters.
// Construction fails fast on structural misconfiguration; Resolve fails on an
// invalid Selection or a Provider that cannot authenticate.
type Registry struct {
	providers map[string]Provider
	auth      map[string]Authentication
	adapters  map[string]Chat
}

// NewRegistry validates the Provider entries against the Authentication store
// and returns a Registry that builds Adapters for them. It fails explicitly
// when an entry declares an unsupported Adapter family, references an unknown
// or unsupported Authentication, omits its base URL or Model Registry, or has
// no name. An entry whose Authentication exists but holds an empty key is
// accepted: the Provider is declared but cannot authenticate until its key
// resolves.
func NewRegistry(providers map[string]Provider, auth map[string]Authentication) (*Registry, error) {
	registry := &Registry{
		providers: make(map[string]Provider, len(providers)),
		auth:      auth,
		adapters:  make(map[string]Chat, len(providers)),
	}
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry := providers[name]
		entry.Name = name
		if err := validateEntry(entry, auth); err != nil {
			return nil, err
		}
		registry.providers[name] = entry
	}
	return registry, nil
}

// validateEntry checks one Provider entry against the Authentication store.
func validateEntry(entry Provider, auth map[string]Authentication) error {
	if entry.Name == "" {
		return fmt.Errorf("provider name must not be empty")
	}
	switch entry.Adapter {
	case AdapterOpenAICompatible:
	default:
		return fmt.Errorf("provider %q declares unsupported adapter family %q (supported: %s)", entry.Name, entry.Adapter, AdapterOpenAICompatible)
	}
	if strings.TrimSpace(entry.Auth) == "" {
		return fmt.Errorf("provider %q declares no authentication; name an entry from the auth module", entry.Name)
	}
	credential, ok := auth[entry.Auth]
	if !ok {
		return fmt.Errorf("provider %q references unknown authentication %q", entry.Name, entry.Auth)
	}
	if credential.Type != AuthAPIKey {
		return fmt.Errorf("authentication %q has unsupported type %q (supported: %s)", entry.Auth, credential.Type, AuthAPIKey)
	}
	if entry.Adapter == AdapterOpenAICompatible && strings.TrimSpace(entry.BaseURL) == "" {
		return fmt.Errorf("provider %q (adapter %s) requires a base URL", entry.Name, AdapterOpenAICompatible)
	}
	if len(entry.Models) == 0 {
		return fmt.Errorf("provider %q declares no models", entry.Name)
	}
	return nil
}

// Resolve returns the Adapter for a Selection, building it on first use. It
// fails explicitly when no Provider or Model is selected, when the Provider
// is unknown, when the Model is not in the Provider's Model Registry, or when
// the Provider cannot authenticate.
func (r *Registry) Resolve(selection Selection) (Chat, error) {
	if r == nil {
		return nil, fmt.Errorf("provider registry is nil")
	}
	if selection.Provider == "" && selection.Model == "" {
		return nil, fmt.Errorf("no Provider or Model is selected")
	}
	if selection.Provider == "" {
		return nil, fmt.Errorf("no Provider is selected")
	}
	if selection.Model == "" {
		return nil, fmt.Errorf("no Model is selected for provider %q", selection.Provider)
	}
	entry, ok := r.providers[selection.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (declared: %s)", selection.Provider, strings.Join(r.names(), ", "))
	}
	if !entry.exposes(selection.Model) {
		return nil, fmt.Errorf("provider %q does not expose model %q (models: %s)", selection.Provider, selection.Model, strings.Join(entry.Models, ", "))
	}
	if !r.canAuthenticate(entry) {
		return nil, fmt.Errorf("provider %q cannot authenticate: authentication %q has no resolved API key", selection.Provider, entry.Auth)
	}
	if adapter, ok := r.adapters[selection.Provider]; ok {
		return adapter, nil
	}
	adapter, err := buildAdapter(entry, r.auth[entry.Auth])
	if err != nil {
		return nil, fmt.Errorf("build adapter for provider %q: %w", selection.Provider, err)
	}
	r.adapters[selection.Provider] = adapter
	return adapter, nil
}

// Providers returns all declared entries in name order.
func (r *Registry) Providers() []Provider {
	if r == nil {
		return nil
	}
	entries := make([]Provider, 0, len(r.providers))
	for _, name := range r.names() {
		entries = append(entries, r.providers[name])
	}
	return entries
}

// Authenticatable returns the Providers the Agent can authenticate to — the
// entries whose Authentication holds a resolved key — in name order. Selection
// should only be offered from these.
func (r *Registry) Authenticatable() []Provider {
	if r == nil {
		return nil
	}
	var entries []Provider
	for _, name := range r.names() {
		entry := r.providers[name]
		if r.canAuthenticate(entry) {
			entries = append(entries, entry)
		}
	}
	return entries
}

// canAuthenticate reports whether the entry's Authentication holds a resolved
// key.
func (r *Registry) canAuthenticate(entry Provider) bool {
	credential, ok := r.auth[entry.Auth]
	return ok && credential.Type == AuthAPIKey && strings.TrimSpace(credential.APIKey) != ""
}

// names returns the registered provider names in order.
func (r *Registry) names() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// exposes reports whether the entry's Model Registry contains model.
func (p Provider) exposes(model string) bool {
	for _, candidate := range p.Models {
		if candidate == model {
			return true
		}
	}
	return false
}

// buildAdapter constructs the Adapter for a validated entry: the wire-format
// translator named by its Adapter family.
func buildAdapter(entry Provider, credential Authentication) (Chat, error) {
	switch entry.Adapter {
	case AdapterOpenAICompatible:
		return NewOpenAICompatible(entry.BaseURL, credential.APIKey), nil
	default:
		return nil, fmt.Errorf("unsupported adapter family %q", entry.Adapter)
	}
}

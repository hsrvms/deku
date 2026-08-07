package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// openAIProvider returns a valid openai-compatible Provider entry for tests.
func openAIProvider(name, baseURL, auth string, models ...string) Provider {
	return Provider{Name: name, Adapter: AdapterOpenAICompatible, BaseURL: baseURL, Auth: auth, Models: models}
}

// collectEvents drains an event stream into a slice.
func registryCollect(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var got []Event
	for event := range events {
		got = append(got, event)
	}
	return got
}

func TestNewRegistryBuildsOpenAICompatibleAdapter(t *testing.T) {
	var authHeader, path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		path = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	registry, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", server.URL+"/v1", "custom", "custom-model")},
		map[string]Authentication{"custom": {Type: AuthAPIKey, APIKey: "sk-custom-key"}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	adapter, err := registry.Resolve(Selection{Provider: "custom", Model: "custom-model"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if _, ok := adapter.(*OpenAICompatible); !ok {
		t.Fatalf("Resolve() adapter = %T, want the OpenAI-compatible Adapter", adapter)
	}

	events, err := adapter.Chat(context.Background(), "custom-model", "", []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	got := registryCollect(t, events)
	if len(got) != 2 {
		t.Fatalf("events = %#v, want a TextDelta and Done", got)
	}
	if authHeader != "Bearer sk-custom-key" {
		t.Errorf("authorization = %q, want the Authentication resolved by name", authHeader)
	}
	if path != "/v1/chat/completions" {
		t.Errorf("request path = %q, want the Provider base URL used", path)
	}
}

func TestRegistryResolvesAuthenticationByName(t *testing.T) {
	keys := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		keys[request.Model] = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	registry, err := NewRegistry(
		map[string]Provider{
			"first":  openAIProvider("first", server.URL, "first-auth", "model-a"),
			"second": openAIProvider("second", server.URL, "second-auth", "model-b"),
		},
		map[string]Authentication{
			"first-auth":  {Type: AuthAPIKey, APIKey: "sk-first"},
			"second-auth": {Type: AuthAPIKey, APIKey: "sk-second"},
		},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	for _, sel := range []Selection{
		{Provider: "first", Model: "model-a"},
		{Provider: "second", Model: "model-b"},
	} {
		adapter, err := registry.Resolve(sel)
		if err != nil {
			t.Fatalf("Resolve(%+v) error = %v", sel, err)
		}
		events, err := adapter.Chat(context.Background(), sel.Model, "", nil, nil)
		if err != nil {
			t.Fatalf("Chat() error = %v", err)
		}
		registryCollect(t, events)
	}
	if keys["model-a"] != "Bearer sk-first" {
		t.Errorf("first provider authorization = %q, want its own Authentication", keys["model-a"])
	}
	if keys["model-b"] != "Bearer sk-second" {
		t.Errorf("second provider authorization = %q, want its own Authentication", keys["model-b"])
	}
}

func TestResolveCachesTheAdapter(t *testing.T) {
	registry, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", "https://api.example.com/v1", "custom", "model-a")},
		map[string]Authentication{"custom": {Type: AuthAPIKey, APIKey: "sk-key"}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	sel := Selection{Provider: "custom", Model: "model-a"}
	first, err := registry.Resolve(sel)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	second, err := registry.Resolve(sel)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if first != second {
		t.Errorf("Resolve() built a new Adapter per call, want the same instance")
	}
}

func TestNewRegistryUnknownAuthenticationFails(t *testing.T) {
	_, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", "https://api.example.com/v1", "missing", "model-a")},
		map[string]Authentication{"other": {Type: AuthAPIKey, APIKey: "sk-key"}},
	)
	if err == nil {
		t.Fatal("expected error for an entry referencing unknown Authentication")
	}
	if !strings.Contains(err.Error(), "custom") || !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %q, want it to name the provider and the unknown authentication", err)
	}
}

func TestNewRegistryMissingAuthenticationFails(t *testing.T) {
	_, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", "https://api.example.com/v1", "", "model-a")},
		map[string]Authentication{},
	)
	if err == nil {
		t.Fatal("expected error for an entry declaring no authentication")
	}
	if !strings.Contains(err.Error(), "custom") {
		t.Errorf("error = %q, want it to name the provider", err)
	}
}

func TestNewRegistryUnsupportedAdapterFamilyFails(t *testing.T) {
	entry := openAIProvider("custom", "https://api.example.com/v1", "custom", "model-a")
	entry.Adapter = "anthropic-messages"
	_, err := NewRegistry(
		map[string]Provider{"custom": entry},
		map[string]Authentication{"custom": {Type: AuthAPIKey, APIKey: "sk-key"}},
	)
	if err == nil {
		t.Fatal("expected error for an unsupported Adapter family")
	}
	if !strings.Contains(err.Error(), "anthropic-messages") {
		t.Errorf("error = %q, want it to name the unsupported family", err)
	}
	if !strings.Contains(err.Error(), AdapterOpenAICompatible) {
		t.Errorf("error = %q, want it to name the supported families", err)
	}
}

func TestNewRegistryUnsupportedAuthTypeFails(t *testing.T) {
	_, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", "https://api.example.com/v1", "custom", "model-a")},
		map[string]Authentication{"custom": {Type: "oauth", APIKey: "sk-key"}},
	)
	if err == nil {
		t.Fatal("expected error for an unsupported authentication type")
	}
	if !strings.Contains(err.Error(), "oauth") {
		t.Errorf("error = %q, want it to name the unsupported type", err)
	}
}

func TestNewRegistryMissingBaseURLFails(t *testing.T) {
	_, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", "", "custom", "model-a")},
		map[string]Authentication{"custom": {Type: AuthAPIKey, APIKey: "sk-key"}},
	)
	if err == nil {
		t.Fatal("expected error for an openai-compatible entry without a base URL")
	}
	if !strings.Contains(err.Error(), "custom") {
		t.Errorf("error = %q, want it to name the provider", err)
	}
}

func TestNewRegistryEmptyModelsFails(t *testing.T) {
	_, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", "https://api.example.com/v1", "custom")},
		map[string]Authentication{"custom": {Type: AuthAPIKey, APIKey: "sk-key"}},
	)
	if err == nil {
		t.Fatal("expected error for an entry declaring no models")
	}
	if !strings.Contains(err.Error(), "custom") {
		t.Errorf("error = %q, want it to name the provider", err)
	}
}

func TestNewRegistryEmptyProviderNameFails(t *testing.T) {
	_, err := NewRegistry(
		map[string]Provider{"": openAIProvider("", "https://api.example.com/v1", "custom", "model-a")},
		map[string]Authentication{"custom": {Type: AuthAPIKey, APIKey: "sk-key"}},
	)
	if err == nil {
		t.Fatal("expected error for an empty provider name")
	}
}

func TestResolveEmptySelectionFails(t *testing.T) {
	registry, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", "https://api.example.com/v1", "custom", "model-a")},
		map[string]Authentication{"custom": {Type: AuthAPIKey, APIKey: "sk-key"}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	if _, err := registry.Resolve(Selection{}); err == nil {
		t.Fatal("expected error for an empty selection")
	} else if !strings.Contains(err.Error(), "no Provider or Model is selected") {
		t.Errorf("error = %q, want the explicit no-selection message", err)
	}
	if _, err := registry.Resolve(Selection{Provider: "custom"}); err == nil {
		t.Fatal("expected error for a selection without a model")
	}
	if _, err := registry.Resolve(Selection{Model: "model-a"}); err == nil {
		t.Fatal("expected error for a selection without a provider")
	}
}

func TestResolveUnknownProviderFails(t *testing.T) {
	registry, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", "https://api.example.com/v1", "custom", "model-a")},
		map[string]Authentication{"custom": {Type: AuthAPIKey, APIKey: "sk-key"}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	_, err = registry.Resolve(Selection{Provider: "ghost", Model: "model-a"})
	if err == nil {
		t.Fatal("expected error for an unknown provider")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error = %q, want it to name the unknown provider", err)
	}
}

func TestResolveModelNotInRegistryFails(t *testing.T) {
	registry, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", "https://api.example.com/v1", "custom", "model-a")},
		map[string]Authentication{"custom": {Type: AuthAPIKey, APIKey: "sk-key"}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	_, err = registry.Resolve(Selection{Provider: "custom", Model: "ghost-model"})
	if err == nil {
		t.Fatal("expected error for a model the provider does not expose")
	}
	if !strings.Contains(err.Error(), "ghost-model") {
		t.Errorf("error = %q, want it to name the unknown model", err)
	}
}

func TestResolveUnauthenticatableProviderFails(t *testing.T) {
	// The auth entry exists but its key did not resolve (empty after
	// substitution): the provider is declared yet cannot authenticate.
	registry, err := NewRegistry(
		map[string]Provider{"custom": openAIProvider("custom", "https://api.example.com/v1", "custom", "model-a")},
		map[string]Authentication{"custom": {Type: AuthAPIKey, APIKey: ""}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	_, err = registry.Resolve(Selection{Provider: "custom", Model: "model-a"})
	if err == nil {
		t.Fatal("expected error when the provider cannot authenticate")
	}
	if !strings.Contains(err.Error(), "cannot authenticate") {
		t.Errorf("error = %q, want it to report the provider cannot authenticate", err)
	}
}

func TestAuthenticatableExcludesUnresolvedKeys(t *testing.T) {
	registry, err := NewRegistry(
		map[string]Provider{
			"broken": openAIProvider("broken", "https://api.example.com/v1", "broken-auth", "model-a"),
			"usable": openAIProvider("usable", "https://api.example.com/v1", "usable-auth", "model-b"),
		},
		map[string]Authentication{
			"broken-auth": {Type: AuthAPIKey, APIKey: ""},
			"usable-auth": {Type: AuthAPIKey, APIKey: "sk-key"},
		},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	got := registry.Authenticatable()
	if len(got) != 1 || got[0].Name != "usable" {
		t.Errorf("Authenticatable() = %#v, want only the provider with a resolved key", got)
	}
}

func TestProvidersListedInNameOrder(t *testing.T) {
	registry, err := NewRegistry(
		map[string]Provider{
			"zeta":  openAIProvider("zeta", "https://api.example.com/v1", "auth", "model-a"),
			"alpha": openAIProvider("alpha", "https://api.example.com/v1", "auth", "model-b"),
		},
		map[string]Authentication{"auth": {Type: AuthAPIKey, APIKey: "sk-key"}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	var names []string
	for _, entry := range registry.Providers() {
		names = append(names, entry.Name)
	}
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(names, want) {
		t.Errorf("Providers() names = %#v, want %#v", names, want)
	}
}

func TestNewRegistryEmptyIsUsable(t *testing.T) {
	registry, err := NewRegistry(nil, nil)
	if err != nil {
		t.Fatalf("NewRegistry(nil, nil) error = %v", err)
	}
	if got := registry.Providers(); len(got) != 0 {
		t.Errorf("Providers() = %#v, want none", got)
	}
	if _, err := registry.Resolve(Selection{Provider: "x", Model: "y"}); err == nil {
		t.Fatal("expected error resolving against an empty registry")
	}
}

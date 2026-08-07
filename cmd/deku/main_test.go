package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsrvms/deku/agent"
	"github.com/hsrvms/deku/config"
)

// writeDekuModules writes Deku Home module files for the current test into a
// fresh HOME directory. An empty body skips that module, so a missing module
// is simply absent.
func writeDekuModules(t *testing.T, settings, auth, models string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dekuDir := filepath.Join(home, ".deku")
	if err := os.MkdirAll(dekuDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"settings.json": settings,
		"auth.json":     auth,
		"models.json":   models,
	} {
		if body == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dekuDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// modelsJSON renders a models module declaring the given providers.
func modelsJSON(providers map[string]any) string {
	data, err := json.Marshal(map[string]any{"providers": providers})
	if err != nil {
		panic(err)
	}
	return string(data)
}

// providerEntry renders one openai-compatible Provider declaration.
func providerEntry(baseURL, auth string, models []string) map[string]any {
	return map[string]any{
		"adapter":  "openai-compatible",
		"base_url": baseURL,
		"auth":     auth,
		"models":   models,
	}
}

// authJSON renders an auth module mapping names to api_key credentials.
func authJSON(keys map[string]string) string {
	entries := make(map[string]any, len(keys))
	for name, key := range keys {
		entries[name] = map[string]any{"type": "api_key", "api_key": key}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// settingsJSON renders a settings module with the default Selection.
func settingsJSON(defaultProvider, defaultModel string) string {
	data, err := json.Marshal(map[string]any{
		"defaultProvider": defaultProvider,
		"defaultModel":    defaultModel,
	})
	if err != nil {
		panic(err)
	}
	return string(data)
}

// modelRequest captures one provider request observed by a test server.
type modelRequest struct {
	Authorization string
	Model         string
	Messages      []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
}

// newModelServer serves one OpenAI-compatible streaming endpoint that answers
// every request with response, recording each request for inspection.
func newModelServer(t *testing.T, response string) (*httptest.Server, *[]modelRequest) {
	t.Helper()
	requests := &[]modelRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		*requests = append(*requests, modelRequest{
			Authorization: r.Header.Get("Authorization"),
			Model:         body.Model,
			Messages:      body.Messages,
		})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", response)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	return server, requests
}

func TestReportGitResult(t *testing.T) {
	var output, errorOutput bytes.Buffer
	if err := reportGitResult(&output, &errorOutput, agent.TurnResult{
		StashRef:   "stash@{0}",
		Validation: &agent.ValidationResult{Command: "go test ./...", Passed: true},
		CommitID:   "deadbeef",
	}); err != nil {
		t.Fatalf("reportGitResult() error = %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "stash@{0}") {
		t.Errorf("output = %q, want stash reference reported", got)
	}
	if !strings.Contains(got, "validation passed") {
		t.Errorf("output = %q, want validation passed reported", got)
	}
	if !strings.Contains(got, "agent commit created deadbeef") {
		t.Errorf("output = %q, want agent commit reported", got)
	}

	output.Reset()
	if err := reportGitResult(&output, &errorOutput, agent.TurnResult{
		Validation: &agent.ValidationResult{Command: "make check", Passed: false},
	}); err != nil {
		t.Fatalf("reportGitResult() error = %v", err)
	}
	if !strings.Contains(output.String(), "validation failed") {
		t.Errorf("failing output = %q, want validation failed reported", output.String())
	}
	if strings.Contains(output.String(), "agent commit") {
		t.Errorf("failing output = %q, must not report a commit", output.String())
	}
}

func TestRunPrintsVersionWithoutProviderConfiguration(t *testing.T) {
	writeDekuModules(t, "", "", "")

	var stdout, stderr bytes.Buffer
	if status := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr); status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	if got, want := stdout.String(), "dev\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if _, err := os.Stat(os.Getenv("HOME") + "/.deku/sessions"); !os.IsNotExist(err) {
		t.Fatalf("sessions directory error = %v, want it not to be created", err)
	}
}

func TestRunStartsConversationAndStreamsResponse(t *testing.T) {
	server, requests := newModelServer(t, "Hi there")
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry(server.URL, "test-auth", []string{"test-model"})}))
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	status := run(nil, strings.NewReader("hello\n"), &stdout, &stderr)
	if status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Hi there") {
		t.Errorf("stdout = %q, want streamed response", stdout.String())
	}
	if len(*requests) != 1 {
		t.Fatalf("provider requests = %d, want one", len(*requests))
	}
	request := (*requests)[0]
	if request.Model != "test-model" {
		t.Errorf("provider model = %q, want the selected model", request.Model)
	}
	if request.Authorization != "Bearer test-key" {
		t.Errorf("authorization = %q, want the provider's resolved Authentication", request.Authorization)
	}
	if len(request.Messages) != 2 || request.Messages[1].Role != "user" || request.Messages[1].Content != "hello" {
		t.Errorf("provider messages = %#v, want system and user messages", request.Messages)
	}
	if !strings.Contains(stderr.String(), "session") {
		t.Errorf("stderr = %q, want session identifier", stderr.String())
	}

	entries, err := os.ReadDir(os.Getenv("HOME") + "/.deku/sessions")
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("session files = %d, want one", len(entries))
	}
}

func TestRunFailsWithoutSelection(t *testing.T) {
	writeDekuModules(t,
		"",
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry("https://api.example.com/v1", "test-auth", []string{"test-model"})}))
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	if status := run(nil, strings.NewReader(""), &stdout, &stderr); status != 1 {
		t.Fatalf("run() status = %d, want 1", status)
	}
	if !strings.Contains(stderr.String(), "no Provider or Model is selected") {
		t.Errorf("stderr = %q, want the explicit no-selection error", stderr.String())
	}
}

func TestRunFailsOnUnknownDefaultProvider(t *testing.T) {
	writeDekuModules(t,
		settingsJSON("ghost", "test-model"),
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry("https://api.example.com/v1", "test-auth", []string{"test-model"})}))
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	if status := run(nil, strings.NewReader(""), &stdout, &stderr); status != 1 {
		t.Fatalf("run() status = %d, want 1", status)
	}
	if !strings.Contains(stderr.String(), "ghost") {
		t.Errorf("stderr = %q, want the unknown default provider named", stderr.String())
	}
}

func TestRunFailsWhenDefaultProviderCannotAuthenticate(t *testing.T) {
	// The auth entry exists but its key placeholder does not resolve: the
	// default Selection fails explicitly at startup.
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"test-auth": "${DEKU_TEST_UNSET_KEY}"}),
		modelsJSON(map[string]any{"test": providerEntry("https://api.example.com/v1", "test-auth", []string{"test-model"})}))
	t.Chdir(t.TempDir())
	_ = os.Unsetenv("DEKU_TEST_UNSET_KEY")

	var stdout, stderr bytes.Buffer
	if status := run(nil, strings.NewReader(""), &stdout, &stderr); status != 1 {
		t.Fatalf("run() status = %d, want 1", status)
	}
	if !strings.Contains(stderr.String(), "cannot authenticate") {
		t.Errorf("stderr = %q, want the authentication failure", stderr.String())
	}
}

func TestModelCommandListsOnlyAuthenticatableProviders(t *testing.T) {
	server, requests := newModelServer(t, "unused")
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{
			"test-auth":   "test-key",
			"broken-auth": "${DEKU_TEST_UNSET_KEY}",
		}),
		modelsJSON(map[string]any{
			"test":   providerEntry(server.URL, "test-auth", []string{"test-model", "other-model"}),
			"broken": providerEntry("https://api.broken.example.com/v1", "broken-auth", []string{"broken-model"}),
		}))
	t.Chdir(t.TempDir())
	_ = os.Unsetenv("DEKU_TEST_UNSET_KEY")

	var stdout, stderr bytes.Buffer
	if status := run(nil, strings.NewReader("/model\n"), &stdout, &stderr); status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "current selection: test / test-model") {
		t.Errorf("stdout = %q, want the current selection reported", got)
	}
	if !strings.Contains(got, "test: test-model, other-model") {
		t.Errorf("stdout = %q, want the authenticatable provider listed with its models", got)
	}
	if strings.Contains(got, "broken") {
		t.Errorf("stdout = %q, must not list a provider the Agent cannot authenticate to", got)
	}
	if len(*requests) != 0 {
		t.Errorf("provider requests = %d, want none: /model never calls the model", len(*requests))
	}
}

func TestModelCommandSwitchesProviderBetweenTurns(t *testing.T) {
	first, firstRequests := newModelServer(t, "first response")
	second, secondRequests := newModelServer(t, "second response")
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"first-auth": "sk-first", "second-auth": "sk-second"}),
		modelsJSON(map[string]any{
			"test":   providerEntry(first.URL, "first-auth", []string{"test-model"}),
			"second": providerEntry(second.URL, "second-auth", []string{"second-model"}),
		}))
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	status := run(nil, strings.NewReader("hello\n/model second second-model\nhello again\n"), &stdout, &stderr)
	if status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "selection: second / second-model") {
		t.Errorf("stdout = %q, want the switch confirmed", stdout.String())
	}
	if !strings.Contains(stdout.String(), "first response") || !strings.Contains(stdout.String(), "second response") {
		t.Errorf("stdout = %q, want both turns answered", stdout.String())
	}
	if len(*firstRequests) != 1 || (*firstRequests)[0].Model != "test-model" || (*firstRequests)[0].Authorization != "Bearer sk-first" {
		t.Errorf("first provider requests = %#v, want the first Turn on the default provider", *firstRequests)
	}
	if len(*secondRequests) != 1 || (*secondRequests)[0].Model != "second-model" || (*secondRequests)[0].Authorization != "Bearer sk-second" {
		t.Errorf("second provider requests = %#v, want the second Turn on the switched provider", *secondRequests)
	}
}

func TestModelOverrideRestoredOnResume(t *testing.T) {
	first, firstRequests := newModelServer(t, "first response")
	second, secondRequests := newModelServer(t, "second response")
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"first-auth": "sk-first", "second-auth": "sk-second"}),
		modelsJSON(map[string]any{
			"test":   providerEntry(first.URL, "first-auth", []string{"test-model"}),
			"second": providerEntry(second.URL, "second-auth", []string{"second-model"}),
		}))
	t.Chdir(t.TempDir())

	var firstOutput, firstErrors bytes.Buffer
	if status := run(nil, strings.NewReader("/model second second-model\nfirst request\n"), &firstOutput, &firstErrors); status != 0 {
		t.Fatalf("first run() status = %d, stderr = %q", status, firstErrors.String())
	}
	fields := strings.Fields(firstErrors.String())
	if len(fields) == 0 {
		t.Fatalf("first stderr = %q, want session ID", firstErrors.String())
	}
	sessionID := fields[len(fields)-1]

	var secondOutput, secondErrors bytes.Buffer
	status := run([]string{"--resume", sessionID}, strings.NewReader("second request\n"), &secondOutput, &secondErrors)
	if status != 0 {
		t.Fatalf("resumed run() status = %d, stderr = %q", status, secondErrors.String())
	}
	if !strings.Contains(secondOutput.String(), "second response") {
		t.Errorf("resumed stdout = %q, want the override provider to answer", secondOutput.String())
	}
	if len(*firstRequests) != 0 {
		t.Errorf("default provider requests = %d, want none after a recorded override", len(*firstRequests))
	}
	if len(*secondRequests) != 2 {
		t.Fatalf("override provider requests = %d, want both Turns on the restored override", len(*secondRequests))
	}
	for index, request := range *secondRequests {
		if request.Model != "second-model" {
			t.Errorf("request %d model = %q, want the restored override model", index, request.Model)
		}
	}
}

func TestModelCommandUnknownProviderReportsError(t *testing.T) {
	server, requests := newModelServer(t, "ok response")
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry(server.URL, "test-auth", []string{"test-model"})}))
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	status := run(nil, strings.NewReader("/model ghost ghost-model\nhello\n"), &stdout, &stderr)
	if status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ghost") {
		t.Errorf("stderr = %q, want the failed switch reported", stderr.String())
	}
	if !strings.Contains(stdout.String(), "ok response") {
		t.Errorf("stdout = %q, want the conversation to continue after a failed switch", stdout.String())
	}
	if len(*requests) != 1 || (*requests)[0].Model != "test-model" {
		t.Errorf("provider requests = %#v, want the unchanged Selection used", *requests)
	}
}

func TestUnknownCommandReportsError(t *testing.T) {
	server, _ := newModelServer(t, "unused")
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry(server.URL, "test-auth", []string{"test-model"})}))
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	if status := run(nil, strings.NewReader("/frobnicate\n"), &stdout, &stderr); status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr = %q, want the unknown command reported", stderr.String())
	}
}

// initGitRepo creates a temporary Git repository and returns its root, so CLI
// tests can place Project Config in a real repository.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

// writeProjectConfig writes a .deku directory with one settings module into
// the repository at root. It repeats the default Selection because a trusted
// project's settings module replaces the Deku Home settings as a whole.
func writeProjectConfig(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".deku"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{ "agent_commits": { "mode": "ask" }, "defaultProvider": "test", "defaultModel": "test-model" }`
	if err := os.WriteFile(filepath.Join(root, ".deku", "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunReportsUntrustedProjectConfig(t *testing.T) {
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry("https://api.example.com/v1", "test-auth", []string{"test-model"})}))
	repo := initGitRepo(t)
	writeProjectConfig(t, repo)
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	if status := run(nil, strings.NewReader(""), &stdout, &stderr); status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not trusted") {
		t.Errorf("stderr = %q, want untrusted project config notice", stderr.String())
	}
}

func TestRunReportsLoadedProjectConfig(t *testing.T) {
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry("https://api.example.com/v1", "test-auth", []string{"test-model"})}))
	repo := initGitRepo(t)
	writeProjectConfig(t, repo)
	data, err := json.Marshal(map[string][]string{"projects": {repo}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".deku"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".deku", "trusted_projects.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	if status := run(nil, strings.NewReader(""), &stdout, &stderr); status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "project config loaded") {
		t.Errorf("stderr = %q, want loaded project config notice", stderr.String())
	}
}

func TestPromptTrust(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    bool
		wantErr bool
	}{
		{in: "y\n", want: true},
		{in: "yes\n", want: true},
		{in: "Y\n", want: true},
		{in: "n\n", want: false},
		{in: "no\n", want: false},
		{in: "\n", want: false},
		{in: "", want: false}, // EOF declines: never trust without consent
		{in: "maybe\ny\n", want: true},
	} {
		var output bytes.Buffer
		got, err := promptTrust(strings.NewReader(tc.in), &output, "/tmp/project")
		if tc.wantErr && err == nil {
			t.Errorf("promptTrust(%q) error = nil, want error", tc.in)
			continue
		}
		if !tc.wantErr && err != nil {
			t.Errorf("promptTrust(%q) error = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("promptTrust(%q) = %v, want %v", tc.in, got, tc.want)
		}
		if !strings.Contains(output.String(), "Trust this project?") {
			t.Errorf("promptTrust(%q) output = %q, want the trust question", tc.in, output.String())
		}
	}
}

func TestResolveProjectTrustInteractiveGrants(t *testing.T) {
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry("https://api.example.com/v1", "test-auth", []string{"test-model"})}))
	home := os.Getenv("HOME")
	repo := initGitRepo(t)
	writeProjectConfig(t, repo)
	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	var output bytes.Buffer
	resolved, err := resolveProjectTrust(cfg, repo, strings.NewReader("y\n"), &output, true)
	if err != nil {
		t.Fatalf("resolveProjectTrust() error = %v", err)
	}
	if !resolved.Project.Loaded {
		t.Errorf("resolved project scope = %+v, want loaded after interactive grant", resolved.Project)
	}
	if resolved.AgentCommits.Mode != "ask" {
		t.Errorf("agent_commits.mode = %q, want project value after interactive grant", resolved.AgentCommits.Mode)
	}
	data, err := os.ReadFile(filepath.Join(home, ".deku", "trusted_projects.json"))
	if err != nil {
		t.Fatalf("trust record was not written: %v", err)
	}
	if !strings.Contains(string(data), repo) {
		t.Errorf("trust record = %s, want it to list %s", data, repo)
	}
}

func TestResolveProjectTrustInteractiveDeclines(t *testing.T) {
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry("https://api.example.com/v1", "test-auth", []string{"test-model"})}))
	home := os.Getenv("HOME")
	repo := initGitRepo(t)
	writeProjectConfig(t, repo)
	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	var output bytes.Buffer
	resolved, err := resolveProjectTrust(cfg, repo, strings.NewReader("n\n"), &output, true)
	if err != nil {
		t.Fatalf("resolveProjectTrust() error = %v", err)
	}
	if resolved.Project.Trusted || resolved.Project.Loaded {
		t.Errorf("resolved project scope = %+v, want untrusted after decline", resolved.Project)
	}
	if _, err := os.Stat(filepath.Join(home, ".deku", "trusted_projects.json")); !os.IsNotExist(err) {
		t.Errorf("trust record exists after decline, want none")
	}
}

func TestResolveProjectTrustNonInteractiveSkipsPrompt(t *testing.T) {
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry("https://api.example.com/v1", "test-auth", []string{"test-model"})}))
	repo := initGitRepo(t)
	writeProjectConfig(t, repo)
	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	var output bytes.Buffer
	resolved, err := resolveProjectTrust(cfg, repo, strings.NewReader(""), &output, false)
	if err != nil {
		t.Fatalf("resolveProjectTrust() error = %v", err)
	}
	if resolved != cfg {
		t.Errorf("non-interactive resolution changed the config")
	}
	if output.Len() != 0 {
		t.Errorf("non-interactive resolution output = %q, want no prompt", output.String())
	}
}

func TestRunResumesSessionHistory(t *testing.T) {
	requests := &[]modelRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		*requests = append(*requests, modelRequest{Model: body.Model, Messages: body.Messages})
		response := "first response"
		if len(*requests) > 1 {
			response = "second response"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", response)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	writeDekuModules(t,
		settingsJSON("test", "test-model"),
		authJSON(map[string]string{"test-auth": "test-key"}),
		modelsJSON(map[string]any{"test": providerEntry(server.URL, "test-auth", []string{"test-model"})}))
	t.Chdir(t.TempDir())

	var firstOutput, firstErrors bytes.Buffer
	if status := run(nil, strings.NewReader("first request\n"), &firstOutput, &firstErrors); status != 0 {
		t.Fatalf("first run() status = %d, stderr = %q", status, firstErrors.String())
	}
	fields := strings.Fields(firstErrors.String())
	if len(fields) == 0 {
		t.Fatalf("first stderr = %q, want session ID", firstErrors.String())
	}
	sessionID := fields[len(fields)-1]

	var secondOutput, secondErrors bytes.Buffer
	status := run([]string{"--resume", sessionID}, strings.NewReader("second request\n"), &secondOutput, &secondErrors)
	if status != 0 {
		t.Fatalf("second run() status = %d, stderr = %q", status, secondErrors.String())
	}
	if !strings.Contains(secondOutput.String(), "second response") {
		t.Errorf("second stdout = %q, want resumed response", secondOutput.String())
	}
	if len(*requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(*requests))
	}
	if len((*requests)[1].Messages) != 4 {
		t.Fatalf("resumed provider messages = %d, want 4 including system prompt", len((*requests)[1].Messages))
	}
	want := []string{"first request", "first response", "second request"}
	for index, content := range want {
		if (*requests)[1].Messages[index+1].Content != content {
			t.Errorf("resumed message %d = %q, want %q", index, (*requests)[1].Messages[index+1].Content, content)
		}
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsrvms/deku/agent"
	"github.com/hsrvms/deku/config"
	"net/http"
	"net/http/httptest"
)

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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "")
	t.Setenv("DEKU_PROVIDER_API_KEY", "")
	t.Setenv("DEKU_PROVIDER_MODEL", "")

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
	if _, err := os.Stat(home + "/.deku/sessions"); !os.IsNotExist(err) {
		t.Fatalf("sessions directory error = %v, want it not to be created", err)
	}
}

func TestRunStartsConversationAndStreamsResponse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_API_KEY", "test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")

	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi there\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	t.Setenv("DEKU_PROVIDER_ENDPOINT", server.URL)

	var stdout, stderr bytes.Buffer
	status := run(nil, strings.NewReader("hello\n"), &stdout, &stderr)
	if status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Hi there") {
		t.Errorf("stdout = %q, want streamed response", stdout.String())
	}
	if len(request.Messages) != 2 || request.Messages[1].Role != "user" || request.Messages[1].Content != "hello" {
		t.Errorf("provider messages = %#v, want system and user messages", request.Messages)
	}
	if !strings.Contains(stderr.String(), "session") {
		t.Errorf("stderr = %q, want session identifier", stderr.String())
	}

	entries, err := os.ReadDir(home + "/.deku/sessions")
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("session files = %d, want one", len(entries))
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
// the repository at root.
func writeProjectConfig(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".deku"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{ "agent_commits": { "mode": "ask" } }`
	if err := os.WriteFile(filepath.Join(root, ".deku", "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunReportsUntrustedProjectConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")
	repo := initGitRepo(t)
	writeProjectConfig(t, repo)
	data, err := json.Marshal(map[string][]string{"projects": {repo}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".deku"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".deku", "trusted_projects.json"), data, 0o644); err != nil {
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_ENDPOINT", "https://api.example.com/v1")
	t.Setenv("DEKU_PROVIDER_API_KEY", "test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_API_KEY", "test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")

	var requests []struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		requests = append(requests, request)
		response := "first response"
		if len(requests) > 1 {
			response = "second response"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", response)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	t.Setenv("DEKU_PROVIDER_ENDPOINT", server.URL)

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
	if status := run([]string{"--resume", sessionID}, strings.NewReader("second request\n"), &secondOutput, &secondErrors); status != 0 {
		t.Fatalf("second run() status = %d, stderr = %q", status, secondErrors.String())
	}
	if !strings.Contains(secondOutput.String(), "second response") {
		t.Errorf("second stdout = %q, want resumed response", secondOutput.String())
	}
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	if len(requests[1].Messages) != 4 {
		t.Fatalf("resumed provider messages = %d, want 4 including system prompt", len(requests[1].Messages))
	}
	want := []string{"first request", "first response", "second request"}
	for index, content := range want {
		if requests[1].Messages[index+1].Content != content {
			t.Errorf("resumed message %d = %q, want %q", index, requests[1].Messages[index+1].Content, content)
		}
	}
}

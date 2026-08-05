package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/repository"
	"github.com/hsrvms/deku/session"
	"github.com/hsrvms/deku/tool"
)

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out.String())
	}
	return out.String()
}

// newGitRepo creates a temporary Git repository containing a minimal compiling
// Go module and an initial commit. The default Validation command (`go test
// ./...`) passes for an unchanged repository.
func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Test")
	files := map[string]string{
		"go.mod":  "module example.com/t\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {}\n",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(t, dir, "add", "--", "go.mod", "main.go")
	mustGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// newGitAgent wires an Agent to a real temporary Git repository, the primary
// Repository-safety test seam. input carries all synchronous user responses in
// order (dirty-tree choice, tool approvals, and commit decisions).
func newGitAgent(t *testing.T, dir string, providerStub provider.Chat, input string, mode repository.Mode, validation string) (*Agent, *session.Session, *bytes.Buffer) {
	t.Helper()
	registry, err := tool.NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	var output bytes.Buffer
	repo, err := repository.New(dir)
	if err != nil {
		t.Fatalf("repository.New() error = %v", err)
	}
	runner := NewWithGit(providerStub, "test-model", conversation, &output, strings.NewReader(input), registry, approval.DefaultPolicy(), nil, repo, mode, validation)
	return runner, conversation, &output
}

func gitLog(t *testing.T, dir string) []string {
	t.Helper()
	out := mustGit(t, dir, "log", "--format=%s", "-n", "20")
	var messages []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line != "" {
			messages = append(messages, line)
		}
	}
	return messages
}

// gitShowStat returns the `--stat` summary of the commit at rev.
func gitShowStat(t *testing.T, dir, rev string) string {
	t.Helper()
	return mustGit(t, dir, "show", "--stat", "--format=", rev)
}

func TestAgentGitCleanStartCreatesAgentCommit(t *testing.T) {
	dir := newGitRepo(t)
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func main() { println(\"done\") }"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Updated."}, provider.Done{}},
		},
	}
	runner, _, _ := newGitAgent(t, dir, providerStub, "y\n", repository.ModeOn, "true")

	result, err := runner.Turn(context.Background(), "Make main print done.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "Updated." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	if result.CommitID == "" {
		t.Fatal("CommitID is empty, want an Agent Commit")
	}
	if result.Validation == nil || !result.Validation.Passed {
		t.Fatalf("Validation = %#v, want passed", result.Validation)
	}
	messages := gitLog(t, dir)
	if len(messages) < 2 {
		t.Fatalf("git log = %#v, want init plus agent commit", messages)
	}
	if !strings.Contains(messages[0], "deku:") {
		t.Errorf("latest commit message = %q, want deku agent commit", messages[0])
	}
	// The commit must contain only the Agent-edited file.
	stat := gitShowStat(t, dir, "HEAD")
	if !strings.Contains(stat, "main.go") {
		t.Errorf("agent commit stat = %q, want main.go", stat)
	}
	// The working tree is clean after the commit.
	status := mustGit(t, dir, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Errorf("working tree not clean after commit: %q", status)
	}
}

func TestAgentGitDirtyStartWithCheckpoint(t *testing.T) {
	dir := newGitRepo(t)
	// Pre-existing change that the Agent must not silently absorb.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("user wip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func main() { println(\"done\") }"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Updated."}, provider.Done{}},
		},
	}
	// 1 = Checkpoint, then approve the edit.
	runner, _, _ := newGitAgent(t, dir, providerStub, "1\ny\n", repository.ModeOn, "true")

	result, err := runner.Turn(context.Background(), "Make main print done.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.CommitID == "" {
		t.Fatal("CommitID is empty, want an Agent Commit")
	}
	messages := gitLog(t, dir)
	if len(messages) < 3 {
		t.Fatalf("git log = %#v, want init, checkpoint, agent commit", messages)
	}
	// The checkpoint must preserve the user's README.md.
	checkpoint := messages[1]
	if !strings.Contains(checkpoint, "checkpoint") {
		t.Errorf("checkpoint message = %q, want a checkpoint message", checkpoint)
	}
	readmeStat := gitShowStat(t, dir, "HEAD~1")
	if !strings.Contains(readmeStat, "README.md") {
		t.Errorf("checkpoint stat = %q, want README.md preserved", readmeStat)
	}
	if strings.Contains(readmeStat, "main.go") {
		t.Errorf("checkpoint stat = %q, should not include the agent-edited main.go", readmeStat)
	}
}

func TestAgentGitDirtyStartWithStash(t *testing.T) {
	dir := newGitRepo(t)
	// Pre-existing change to stash out of the way.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("user wip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func main() { println(\"done\") }"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Updated."}, provider.Done{}},
		},
	}
	// 2 = Stash, then approve the edit.
	runner, _, _ := newGitAgent(t, dir, providerStub, "2\ny\n", repository.ModeOn, "true")

	result, err := runner.Turn(context.Background(), "Make main print done.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.CommitID == "" {
		t.Fatal("CommitID is empty, want an Agent Commit")
	}
	if result.StashRef == "" {
		t.Fatal("StashRef is empty, want the identifiable stash reference")
	}
	if !strings.HasPrefix(result.StashRef, "stash@{") {
		t.Errorf("StashRef = %q, want a stash reference", result.StashRef)
	}
	list := mustGit(t, dir, "stash", "list")
	if !strings.Contains(list, "deku:") {
		t.Errorf("stash list = %q, want a recognizable deku stash message", list)
	}
	messages := gitLog(t, dir)
	if !strings.Contains(messages[0], "deku:") {
		t.Errorf("latest commit = %q, want agent commit", messages[0])
	}
}

func TestAgentGitDirtyStartWithCancel(t *testing.T) {
	dir := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("user wip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.TextDelta{Text: "unused"}, provider.Done{}},
		},
	}
	// 4 = Cancel.
	runner, _, _ := newGitAgent(t, dir, providerStub, "4\n", repository.ModeOn, "true")

	if _, err := runner.Turn(context.Background(), "Make main print done."); err == nil {
		t.Fatal("Turn() error = nil, want cancellation error")
	}
	// Provider must never have been called, and no turn was recorded.
	if providerStub.calls != 0 {
		t.Errorf("provider calls = %d, want 0 after cancellation", providerStub.calls)
	}
	if messages := gitLog(t, dir); len(messages) != 1 {
		t.Errorf("git log = %#v, want only init", messages)
	}
}

func TestAgentGitDirtyStartContinueWithoutCommits(t *testing.T) {
	dir := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("user wip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func main() { println(\"done\") }"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Updated."}, provider.Done{}},
		},
	}
	// 3 = Continue without Agent Commits, then approve the edit.
	runner, _, _ := newGitAgent(t, dir, providerStub, "3\ny\n", repository.ModeOn, "true")

	result, err := runner.Turn(context.Background(), "Make main print done.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.CommitID != "" {
		t.Errorf("CommitID = %q, want no commit when commits are disabled", result.CommitID)
	}
	if messages := gitLog(t, dir); len(messages) != 1 {
		t.Errorf("git log = %#v, want only init", messages)
	}
	// The Agent's edit still happened on disk, uncommitted.
	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "println") {
		t.Errorf("main.go after turn = %q, want the agent edit to remain on disk", data)
	}
}

func TestAgentGitValidationFailureLeavesWorkUncommitted(t *testing.T) {
	dir := newGitRepo(t)
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func main() { println(\"done\") }"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Updated."}, provider.Done{}},
		},
	}
	runner, _, _ := newGitAgent(t, dir, providerStub, "y\n", repository.ModeOn, "false")

	result, err := runner.Turn(context.Background(), "Make main print done.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Validation == nil || result.Validation.Passed {
		t.Fatalf("Validation = %#v, want failed", result.Validation)
	}
	if result.CommitID != "" {
		t.Errorf("CommitID = %q, want no commit on failed Validation", result.CommitID)
	}
	if messages := gitLog(t, dir); len(messages) != 1 {
		t.Errorf("git log = %#v, want only init", messages)
	}
	// The agent's change must remain on disk for inspection.
	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "println") {
		t.Errorf("main.go = %q, want uncommitted agent change preserved", data)
	}
}

func TestAgentGitRunsRealValidationBeforeCommit(t *testing.T) {
	dir := newGitRepo(t)
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func main() { println(\"done\") }"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Updated."}, provider.Done{}},
		},
	}
	// The default validation command is `go test ./...`; pass an empty command
	// to exercise the real command path.
	runner, _, _ := newGitAgent(t, dir, providerStub, "y\n", repository.ModeOn, "")
	result, err := runner.Turn(context.Background(), "Make main print done.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Validation == nil || !result.Validation.Passed || result.Validation.Command != "go test ./..." {
		t.Fatalf("Validation = %#v, want passing go test ./...", result.Validation)
	}
	if result.CommitID == "" {
		t.Fatal("CommitID is empty, want an Agent Commit after passing Validation")
	}
}

func TestAgentGitPausesOnExternalChange(t *testing.T) {
	dir := newGitRepo(t)
	// An external actor modifies notes.txt during the Turn, in the provider's
	// first Chat call, while the Agent edits main.go.
	providerStub := &externalChangeProvider{
		externalPath: filepath.Join(dir, "notes.txt"),
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func main() { println(\"done\") }"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Updated."}, provider.Done{}},
		},
	}
	runner, _, _ := newGitAgent(t, dir, providerStub, "y\n", repository.ModeOn, "true")

	_, err := runner.Turn(context.Background(), "Make main print done.")
	if err == nil {
		t.Fatal("Turn() error = nil, want pause on external change")
	}
	if !strings.Contains(err.Error(), "external") {
		t.Errorf("error = %q, want an external-change pause message", err)
	}
	if messages := gitLog(t, dir); len(messages) != 1 {
		t.Errorf("git log = %#v, want no Agent Commit after external change", messages)
	}
}

func TestAgentGitInterruptionLeavesWorkUncommitted(t *testing.T) {
	dir := newGitRepo(t)
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func main() { println(\"done\") }"}]}`}, provider.Done{}},
			{provider.Error{Err: errors.New("provider stream failed mid-turn")}},
		},
	}
	runner, _, _ := newGitAgent(t, dir, providerStub, "y\n", repository.ModeOn, "true")

	_, err := runner.Turn(context.Background(), "Make main print done.")
	if err == nil {
		t.Fatal("Turn() error = nil, want provider error surfaced")
	}
	if messages := gitLog(t, dir); len(messages) != 1 {
		t.Errorf("git log = %#v, want no commit after interrupted Turn", messages)
	}
}

func TestAgentGitAskModePromptsBeforeCommit(t *testing.T) {
	dir := newGitRepo(t)
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func main() { println(\"done\") }"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Updated."}, provider.Done{}},
		},
	}
	// approve the edit, then approve the Agent Commit.
	runner, _, output := newGitAgent(t, dir, providerStub, "y\ny\n", repository.ModeAsk, "true")

	result, err := runner.Turn(context.Background(), "Make main print done.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.CommitID == "" {
		t.Fatal("CommitID is empty, want an approved Agent Commit")
	}
	if !strings.Contains(output.String(), "Agent Commit") {
		t.Errorf("output = %q, want an Agent Commit prompt", output.String())
	}
}

func TestAgentGitAskModeDeclinesCommit(t *testing.T) {
	dir := newGitRepo(t)
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func main() { println(\"done\") }"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Updated."}, provider.Done{}},
		},
	}
	// approve the edit, then decline the Agent Commit.
	runner, _, _ := newGitAgent(t, dir, providerStub, "y\nn\n", repository.ModeAsk, "true")

	result, err := runner.Turn(context.Background(), "Make main print done.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.CommitID != "" {
		t.Errorf("CommitID = %q, want no commit when the user declines", result.CommitID)
	}
	if messages := gitLog(t, dir); len(messages) != 1 {
		t.Errorf("git log = %#v, want only init", messages)
	}
	// The edit remains uncommitted for the user.
	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "println") {
		t.Errorf("main.go = %q, want uncommitted agent change preserved", data)
	}
}

// externalChangeProvider records Agent-touched files like continuationProvider
// and additionally writes to an external path during the first Chat call.
type externalChangeProvider struct {
	externalPath string
	responses    [][]provider.Event
	requests     []providerRequest
	calls        int
}

func (p *externalChangeProvider) Chat(_ context.Context, _ string, system string, messages []provider.Message, tools []provider.ToolDefinition) (<-chan provider.Event, error) {
	if p.calls >= len(p.responses) {
		return nil, errors.New("unexpected provider call")
	}
	if p.calls == 0 {
		if err := os.WriteFile(p.externalPath, []byte("changed externally\n"), 0o600); err != nil {
			return nil, err
		}
	}
	p.requests = append(p.requests, providerRequest{
		system:   system,
		messages: append([]provider.Message(nil), messages...),
		tools:    append([]provider.ToolDefinition(nil), tools...),
	})
	events := make(chan provider.Event, len(p.responses[p.calls]))
	for _, event := range p.responses[p.calls] {
		events <- event
	}
	close(events)
	p.calls++
	return events, nil
}

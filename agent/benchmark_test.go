package agent

import (
	"bytes"
	"context"
	"fmt"
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

// fixtureGoTest runs `go test ./...` in the fixture root and reports whether it
// passes together with its captured output.
func fixtureGoTest(t *testing.T, dir string) (bool, string) {
	t.Helper()
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

// resultOutcome runs a complete Turn through the Agent seam against a real
// temporary Git repository and returns the TurnResult plus the provider-call
// count observed by a wrapped Provider.
func resultOutcome(t *testing.T, stub provider.Chat, dir string, input string, mode repository.Mode, validation string) (TurnResult, int) {
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
	repo, err := repository.New(dir)
	if err != nil {
		t.Fatalf("repository.New() error = %v", err)
	}
	counting := &countingProvider{inner: stub}
	var output bytes.Buffer
	runner := NewWithGit(counting, "test-model", conversation, &output, strings.NewReader(input), registry, approval.DefaultPolicy(), nil, repo, mode, validation)
	result, err := runner.Turn(context.Background(), benchmarkRequest)
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	return result, counting.calls
}

// benchmarkRequest is the user request used for the fixture and the benchmark.
// It directs the model at the failing test and the required outcome without
// leaking the root cause, and deliberately leaves the Agent Commit to Deku so
// the model does not perform its own Git operations through the command tool.
const benchmarkRequest = "Run the test suite, find the failing test, identify the root cause, and fix it so `go test ./...` passes."

// countingProvider wraps a Provider and counts how many times Chat is invoked,
// so the benchmark can enforce the Provider-call budget.
type countingProvider struct {
	inner provider.Chat
	calls int
}

func (p *countingProvider) Chat(ctx context.Context, model, system string, messages []provider.Message, tools []provider.ToolDefinition) (<-chan provider.Event, error) {
	p.calls++
	return p.inner.Chat(ctx, model, system, messages, tools)
}

// TestFixtureSeededWithFailingTest verifies the benchmark fixture is correctly
// seeded: it is a clean committed repository of roughly 30 source files whose
// `go test ./...` fails only because of the bug in stats.Mean.
func TestFixtureSeededWithFailingTest(t *testing.T) {
	dir := seedFixture(t)

	committed := mustGit(t, dir, "ls-files")
	var goFiles int
	for _, path := range strings.Split(strings.TrimRight(committed, "\n"), "\n") {
		if strings.HasSuffix(path, ".go") {
			goFiles++
		}
	}
	if goFiles < 28 || goFiles > 34 {
		t.Fatalf("fixture Go source file count = %d, want roughly 30 source files", goFiles)
	}
	if status := strings.TrimSpace(mustGit(t, dir, "status", "--porcelain")); status != "" {
		t.Fatalf("fixture repo is not clean at seed: %q", status)
	}
	meanGo, err := os.ReadFile(filepath.Join(dir, "stats", "mean.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(meanGo), fixtureBuggyLine) {
		t.Fatalf("fixture does not seed the buggy line in stats/mean.go")
	}

	passed, output := fixtureGoTest(t, dir)
	if passed {
		t.Fatalf("seeded fixture's `go test ./...` passed; want the seeded test to fail")
	}
	if !strings.Contains(output, "TestMean") {
		t.Errorf("failing test output = %q, want TestMean to fail", output)
	}
}

// TestFixtureFullTurnFixesAndCommits drives a complete deterministic Turn
// through the Agent seam against the seeded fixture. A scripted Provider reads
// the failing test, applies the exact fix, and returns a final response; Deku
// then runs real Validation and creates an Agent Commit containing only the fix.
func TestFixtureFullTurnFixesAndCommits(t *testing.T) {
	dir := seedFixture(t)
	stub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "read", Arguments: `{"path":"stats/stats_test.go"}`}, provider.Done{}},
			{provider.ToolCall{ID: "call-2", Name: "edit", Arguments: fmt.Sprintf(`{"path":"stats/mean.go","edits":[{"oldText":%q,"newText":%q}]}`, fixtureBuggyLine, fixtureBugFix)}, provider.Done{}},
			{provider.TextDelta{Text: "Fixed the mean divider and verified the tests pass."}, provider.Done{}},
		},
	}

	// The Agent reads the failing test (read-only, no prompt), edits mean.go
	// (Write tier, approve with y), then reports the final response.
	result, providerCalls := resultOutcome(t, stub, dir, "y\n", repository.ModeOn, "go test ./...")

	if providerCalls != 3 {
		t.Errorf("provider calls = %d, want 3 (read, edit, final)", providerCalls)
	}
	if result.CommitID == "" {
		t.Fatal("CommitID is empty, want an Agent Commit containing the fix")
	}
	if result.Validation == nil || !result.Validation.Passed {
		t.Fatalf("Validation = %#v, want passing go test ./...", result.Validation)
	}

	passed, output := fixtureGoTest(t, dir)
	if !passed {
		t.Fatalf("`go test ./...` failed after the fixed Turn:\n%s", output)
	}

	// The Agent Commit must contain only the file the Agent fixed.
	files := strings.Split(strings.TrimRight(mustGit(t, dir, "show", "--name-only", "--format=", "HEAD"), "\n"), "\n")
	if len(files) != 1 || files[0] != "stats/mean.go" {
		t.Errorf("agent commit files = %#v, want only [stats/mean.go]", files)
	}
	if status := strings.TrimSpace(mustGit(t, dir, "status", "--porcelain")); status != "" {
		t.Errorf("working tree not clean after commit: %q", status)
	}
}

// TestV0Benchmark is the v0 acceptance benchmark. It runs a real compatible
// Provider over the seeded fixture and records Provider-call and billed-token
// metrics, enforcing the v0 targets (at most 8 Provider calls and 60,000 total
// billed tokens). It is opt-in: it runs only when DEKU_BENCHMARK=1 and the
// Provider is configured, so deterministic test runs never claim model quality
// or billed-token compliance.
func TestV0Benchmark(t *testing.T) {
	if os.Getenv("DEKU_BENCHMARK") != "1" {
		t.Skip("v0 benchmark is opt-in; set DEKU_BENCHMARK=1 and configure a Provider to run it")
	}
	endpoint := os.Getenv("DEKU_PROVIDER_ENDPOINT")
	apiKey := os.Getenv("DEKU_PROVIDER_API_KEY")
	model := os.Getenv("DEKU_PROVIDER_MODEL")
	if endpoint == "" || apiKey == "" || model == "" {
		t.Fatal("DEKU_BENCHMARK=1 requires DEKU_PROVIDER_ENDPOINT, DEKU_PROVIDER_API_KEY, and DEKU_PROVIDER_MODEL")
	}

	dir := seedFixture(t)
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
	repo, err := repository.New(dir)
	if err != nil {
		t.Fatalf("repository.New() error = %v", err)
	}

	// Auto-approve every tool so the benchmark can run unattended; the model
	// may use the destructive `command` tool to run its own tests.
	policy := approval.NewPolicy(nil, map[approval.Tier]approval.Action{
		approval.Read:        approval.Auto,
		approval.Write:       approval.Auto,
		approval.Destructive: approval.Auto,
	})
	counting := &countingProvider{inner: provider.NewOpenAICompatible(endpoint, apiKey)}
	var output bytes.Buffer
	runner := NewWithGit(counting, model, conversation, &output, strings.NewReader(""), registry, policy, nil, repo, repository.ModeOn, "go test ./...")

	result, err := runner.Turn(context.Background(), benchmarkRequest)
	if err != nil {
		dumpGitState(t, dir)
		t.Fatalf("benchmark Turn() error = %v", err)
	}

	// Outcome: the fix must be committed and `go test ./...` must pass.
	if result.CommitID == "" {
		t.Fatal("no Agent Commit created; the fix was not committed")
	}
	if result.Validation == nil || !result.Validation.Passed {
		t.Fatalf("Validation = %#v, want go test ./... to pass on the fixed repository", result.Validation)
	}
	passed, testOutput := fixtureGoTest(t, dir)
	if !passed {
		t.Fatalf("`go test ./...` failed at benchmark completion:\n%s", testOutput)
	}
	files := strings.Split(strings.TrimRight(mustGit(t, dir, "show", "--name-only", "--format=", "HEAD"), "\n"), "\n")
	if len(files) != 1 || files[0] != "stats/mean.go" {
		t.Errorf("agent commit files = %#v, want only the fixed stats/mean.go", files)
	}

	// Metrics. A Provider that reports no usage cannot establish benchmark
	// compliance, so the run must fail rather than pass the token target by
	// defaulting to zero.
	if result.Usage == nil || result.Usage.TotalTokens == 0 {
		t.Fatal("Provider reported no usage; benchmark cannot establish token compliance")
	}
	billed := result.Usage.TotalTokens
	t.Logf("benchmark metrics: provider_calls=%d prompt_tokens=%d completion_tokens=%d total_tokens=%d",
		counting.calls, promptTokens(result.Usage), completionTokens(result.Usage), billed)

	if counting.calls > 8 {
		t.Errorf("benchmark used %d Provider calls, exceeding the v0 target of 8", counting.calls)
	}
	if billed > 60000 {
		t.Errorf("benchmark billed %d total tokens, exceeding the v0 target of 60,000", billed)
	}
}

func promptTokens(u *provider.Usage) int {
	if u == nil {
		return 0
	}
	return u.PromptTokens
}

func completionTokens(u *provider.Usage) int {
	if u == nil {
		return 0
	}
	return u.CompletionTokens
}

// dumpGitState logs the repository working-tree and index state so a failed
// real-provider benchmark run can be diagnosed without rerunning the model.
func dumpGitState(t *testing.T, dir string) {
	t.Helper()
	dump := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, _ := cmd.CombinedOutput()
		return string(out)
	}
	t.Logf("--- git status ---\n%s", dump("status"))
	t.Logf("--- git diff --cached ---\n%s", dump("diff", "--cached"))
	t.Logf("--- git log ---\n%s", dump("log", "--oneline", "-n", "5"))
}

package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hsrvms/deku/activity"
	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/session"
	"github.com/hsrvms/deku/tool"
)

// recordingSink is a fake activity Sink that records the emitted stream in
// order so a test can assert a deterministic indicator-and-change sequence.
type recordingSink struct {
	indicators []activity.Indicator
	tools      []string
	changes    []activity.Change
}

func (s *recordingSink) Indicator(i activity.Indicator) { s.indicators = append(s.indicators, i) }
func (s *recordingSink) ActiveTool(name string)         { s.tools = append(s.tools, name) }
func (s *recordingSink) Change(c activity.Change)       { s.changes = append(s.changes, c) }

func newActivityAgent(t *testing.T, root string, providerStub provider.Chat, input string, sink activity.Sink) (*Agent, *session.Session) {
	t.Helper()
	registry, err := tool.NewRegistry(root)
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
	runner := NewWithActivity(providerStub, "test-model", conversation, &output, strings.NewReader(input), registry, approval.DefaultPolicy(), nil, sink)
	return runner, conversation
}

func TestAgentEmitsDeterministicActivityStreamForWriteTurn(t *testing.T) {
	root := t.TempDir()
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "write", Arguments: `{"path":"notes.txt","content":"hello\n"}`}, provider.Done{}},
			{provider.TextDelta{Text: "Created notes.txt."}, provider.Done{}},
		},
	}
	sink := &recordingSink{}
	runner, _ := newActivityAgent(t, root, providerStub, "y\n", sink)

	result, err := runner.Turn(context.Background(), "Create notes.txt.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "Created notes.txt." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); err != nil {
		t.Fatalf("approved write did not create the file: %v", err)
	}

	wantIndicators := []activity.Indicator{
		activity.Thinking,
		activity.AwaitingApproval,
		activity.Working,
		activity.Thinking,
	}
	if !reflect.DeepEqual(sink.indicators, wantIndicators) {
		t.Errorf("indicators = %#v, want %#v", sink.indicators, wantIndicators)
	}
	wantChanges := []activity.Change{{Tool: "write", Path: "notes.txt"}}
	if !reflect.DeepEqual(sink.changes, wantChanges) {
		t.Errorf("changes = %#v, want %#v", sink.changes, wantChanges)
	}
	wantTools := []string{"write"}
	if !reflect.DeepEqual(sink.tools, wantTools) {
		t.Errorf("active tools = %#v, want %#v", sink.tools, wantTools)
	}
}

func TestAgentEmitsWorkingStreamWithoutChangeForReadTool(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "read", Arguments: `{"path":"main.go"}`}, provider.Done{}},
			{provider.TextDelta{Text: "Inspected main.go."}, provider.Done{}},
		},
	}
	sink := &recordingSink{}
	runner, _ := newActivityAgent(t, root, providerStub, "", sink)

	if _, err := runner.Turn(context.Background(), "Inspect main.go."); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}

	// A purely auto-approved call never pauses the loop for a decision, so
	// it must not emit the awaiting-approval indicator: Thinking → Working
	// directly (CONTEXT.md: awaiting Approval is the state where the loop is
	// paused for a user decision).
	wantIndicators := []activity.Indicator{
		activity.Thinking,
		activity.Working,
		activity.Thinking,
	}
	if !reflect.DeepEqual(sink.indicators, wantIndicators) {
		t.Errorf("indicators = %#v, want %#v", sink.indicators, wantIndicators)
	}
	if len(sink.changes) != 0 {
		t.Errorf("changes = %#v, want none for a read tool", sink.changes)
	}
	wantTools := []string{"read"}
	if !reflect.DeepEqual(sink.tools, wantTools) {
		t.Errorf("active tools = %#v, want %#v", sink.tools, wantTools)
	}
}

func TestAgentEmitsAwaitingApprovalWithoutWorkingForRejectedTool(t *testing.T) {
	root := t.TempDir()
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "write", Arguments: `{"path":"notes.txt","content":"hello\n"}`}, provider.Done{}},
			{provider.TextDelta{Text: "The write was declined."}, provider.Done{}},
		},
	}
	sink := &recordingSink{}
	runner, _ := newActivityAgent(t, root, providerStub, "n\n", sink)

	if _, err := runner.Turn(context.Background(), "Create notes.txt."); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}

	wantIndicators := []activity.Indicator{
		activity.Thinking,
		activity.AwaitingApproval,
		activity.Thinking,
	}
	if !reflect.DeepEqual(sink.indicators, wantIndicators) {
		t.Errorf("indicators = %#v, want %#v", sink.indicators, wantIndicators)
	}
	if len(sink.changes) != 0 {
		t.Errorf("changes = %#v, want none for a rejected tool", sink.changes)
	}
	if len(sink.tools) != 0 {
		t.Errorf("active tools = %#v, want none for a rejected tool", sink.tools)
	}
}

func TestAgentEmitsChangeEventForEachEditAndWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func run() {}"}]}`}, provider.Done{}},
			{provider.ToolCall{ID: "call-2", Name: "write", Arguments: `{"path":"notes.txt","content":"hello\n"}`}, provider.Done{}},
			{provider.TextDelta{Text: "Applied both changes."}, provider.Done{}},
		},
	}
	sink := &recordingSink{}
	runner, _ := newActivityAgent(t, root, providerStub, "y\ny\n", sink)

	if _, err := runner.Turn(context.Background(), "Rename main and add notes."); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}

	wantChanges := []activity.Change{
		{Tool: "edit", Path: "main.go"},
		{Tool: "write", Path: "notes.txt"},
	}
	if !reflect.DeepEqual(sink.changes, wantChanges) {
		t.Errorf("changes = %#v, want %#v", sink.changes, wantChanges)
	}
	wantTools := []string{"edit", "write"}
	if !reflect.DeepEqual(sink.tools, wantTools) {
		t.Errorf("active tools = %#v, want %#v", sink.tools, wantTools)
	}
}

func TestAgentEmitsChangeEventForEveryEditToSamePath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func a() {}"}]}`}, provider.Done{}},
			{provider.ToolCall{ID: "call-2", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func a() {}","newText":"func b() {}"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Applied both edits."}, provider.Done{}},
		},
	}
	sink := &recordingSink{}
	runner, _ := newActivityAgent(t, root, providerStub, "y\ny\n", sink)

	if _, err := runner.Turn(context.Background(), "Rename main twice."); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}

	wantChanges := []activity.Change{
		{Tool: "edit", Path: "main.go"},
		{Tool: "edit", Path: "main.go"},
	}
	if !reflect.DeepEqual(sink.changes, wantChanges) {
		t.Errorf("changes = %#v, want one change event per edit to the same path", sink.changes)
	}
	wantTools := []string{"edit", "edit"}
	if !reflect.DeepEqual(sink.tools, wantTools) {
		t.Errorf("active tools = %#v, want one per executed edit", sink.tools)
	}
}

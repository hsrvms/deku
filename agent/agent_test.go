package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/session"
	"github.com/hsrvms/deku/tool"
)

type scriptedProvider struct {
	system   string
	model    string
	messages []provider.Message
	tools    []provider.ToolDefinition
	events   []provider.Event
}

func (p *scriptedProvider) Chat(_ context.Context, model, system string, messages []provider.Message, tools []provider.ToolDefinition) (<-chan provider.Event, error) {
	p.model = model
	p.system = system
	p.messages = append([]provider.Message(nil), messages...)
	p.tools = append([]provider.ToolDefinition(nil), tools...)
	stream := make(chan provider.Event, len(p.events))
	for _, event := range p.events {
		stream <- event
	}
	close(stream)
	return stream, nil
}

func TestAgentTurnStreamsResponseAndPersistsConversation(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	providerStub := &scriptedProvider{events: []provider.Event{
		provider.TextDelta{Text: "Hello"},
		provider.TextDelta{Text: " from Deku."},
		provider.Done{Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13}},
	}}
	var output bytes.Buffer
	agent := New(providerStub, "test-model", conversation, &output, nil)

	result, err := agent.Turn(context.Background(), "Introduce yourself.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "Hello from Deku." {
		t.Errorf("response = %q, want %q", result.Response, "Hello from Deku.")
	}
	if output.String() != result.Response {
		t.Errorf("streamed output = %q, want %q", output.String(), result.Response)
	}
	if providerStub.model != "test-model" {
		t.Errorf("model = %q, want %q", providerStub.model, "test-model")
	}
	if got := toolNames(providerStub.tools); !reflect.DeepEqual(got, []string{"edit", "grep", "ls", "read"}) {
		t.Errorf("tools = %#v, want edit, grep, ls, read", got)
	}
	if providerStub.system == "" {
		t.Fatal("system prompt is empty")
	}
	if len(providerStub.messages) != 1 || providerStub.messages[0].Role != provider.RoleUser || providerStub.messages[0].Content != "Introduce yourself." {
		t.Fatalf("provider messages = %#v, want one user message", providerStub.messages)
	}

	wantMessages := []session.Message{
		{Role: session.RoleUser, Content: "Introduce yourself."},
		{Role: session.RoleAssistant, Content: "Hello from Deku."},
	}
	if !reflect.DeepEqual(conversation.Messages, wantMessages) {
		t.Errorf("session messages = %#v, want %#v", conversation.Messages, wantMessages)
	}
	resumed, err := store.Resume(conversation.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if !reflect.DeepEqual(resumed.Messages, wantMessages) {
		t.Errorf("resumed messages = %#v, want %#v", resumed.Messages, wantMessages)
	}
}

func TestAgentContinuesToolCallWithReadOnlyToolResult(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "read", Arguments: `{"path":"main.go"}`}, provider.Done{}},
			{provider.TextDelta{Text: "The file defines a main package."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, nil, registry)

	result, err := runner.Turn(context.Background(), "Inspect main.go.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "The file defines a main package." {
		t.Errorf("response = %q, want final model response", result.Response)
	}
	if providerStub.calls != 2 {
		t.Errorf("provider calls = %d, want 2", providerStub.calls)
	}
	if len(providerStub.requests[0].tools) != 4 || len(providerStub.requests[1].tools) != 4 {
		t.Fatalf("tool definitions per step = %d and %d, want 4 and 4", len(providerStub.requests[0].tools), len(providerStub.requests[1].tools))
	}
	secondMessages := providerStub.requests[1].messages
	if len(secondMessages) != 3 {
		t.Fatalf("second-step messages = %#v, want user, assistant Tool Call, tool result", secondMessages)
	}
	if secondMessages[1].Role != provider.RoleAssistant || len(secondMessages[1].ToolCalls) != 1 || secondMessages[1].ToolCalls[0].ID != "call-1" {
		t.Errorf("assistant Tool Call message = %#v", secondMessages[1])
	}
	if secondMessages[2].Role != provider.RoleTool || secondMessages[2].ToolCallID != "call-1" || secondMessages[2].Content != "package main\n\nfunc main() {}\n" {
		t.Errorf("tool result message = %#v", secondMessages[2])
	}
	wantTranscript := []session.Message{
		{Role: session.RoleUser, Content: "Inspect main.go."},
		{Role: session.RoleAssistant, ToolCalls: []session.ToolCall{{ID: "call-1", Name: "read", Arguments: `{"path":"main.go"}`}}, Content: ""},
		{Role: session.RoleTool, ToolCallID: "call-1", Name: "read", Content: "package main\n\nfunc main() {}\n"},
		{Role: session.RoleAssistant, Content: "The file defines a main package."},
	}
	if !reflect.DeepEqual(conversation.Messages, wantTranscript) {
		t.Errorf("session messages = %#v, want %#v", conversation.Messages, wantTranscript)
	}
}

func TestAgentAppliesEditToolCallAndMakesExactMutation(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func run() {}"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Edited the file."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader("y\n"), registry)

	if _, err := runner.Turn(context.Background(), "Rename main to run."); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc run() {}\n" {
		t.Errorf("file after edit = %q, want func run()", got)
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || toolResult.ToolCallID != "call-1" {
		t.Errorf("tool result message = %#v", toolResult)
	}
	if toolResult.Content != "Applied 1 replacement(s) to main.go." {
		t.Errorf("tool result content = %q", toolResult.Content)
	}
}

func TestAgentEditToolFailureLeavesFileUnchanged(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	original := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(file, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
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
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func absent()","newText":"func added()"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "The edit failed."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader("y\n"), registry)
	if runner == nil {
		t.Fatal("NewWithTools() returned nil")
	}
	if _, err := runner.Turn(context.Background(), "Add a function."); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("file mutated after failed edit = %q, want %q", got, original)
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || toolResult.Content == "" {
		t.Errorf("failed edit tool result = %#v", toolResult)
	}
	if !strings.Contains(toolResult.Content, "oldText not found") {
		t.Errorf("failed edit tool result = %q, want mismatch report", toolResult.Content)
	}
}

func TestAgentGatesWriteToolBehindApproval(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func run() {}"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "Applied the approved edit."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader("y\n"), registry)

	result, err := runner.Turn(context.Background(), "Rename main to run.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "Applied the approved edit." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	if !strings.Contains(output.String(), "Approve?") {
		t.Errorf("output = %q, want approval prompt", output.String())
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc run() {}\n" {
		t.Errorf("file after approved edit = %q, want func run()", got)
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || toolResult.Content != "Applied 1 replacement(s) to main.go." {
		t.Errorf("approved tool result = %#v", toolResult)
	}
}

func TestAgentSkipsWriteToolWhenRejected(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	original := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(file, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
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
	providerStub := &continuationProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "edit", Arguments: `{"path":"main.go","edits":[{"oldText":"func main() {}","newText":"func run() {}"}]}`}, provider.Done{}},
			{provider.TextDelta{Text: "The edit was declined."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader("n\n"), registry)

	result, err := runner.Turn(context.Background(), "Rename main to run.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "The edit was declined." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("file after rejected edit = %q, want unchanged", got)
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || !strings.Contains(toolResult.Content, "rejected the edit tool call") {
		t.Errorf("rejected tool result = %#v, want denial reported to model", toolResult)
	}
}

type continuationProvider struct {
	responses [][]provider.Event
	requests  []providerRequest
	calls     int
}

type providerRequest struct {
	messages []provider.Message
	tools    []provider.ToolDefinition
}

func (p *continuationProvider) Chat(_ context.Context, _ string, _ string, messages []provider.Message, tools []provider.ToolDefinition) (<-chan provider.Event, error) {
	if p.calls >= len(p.responses) {
		return nil, errors.New("unexpected provider call")
	}
	p.requests = append(p.requests, providerRequest{
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

func toolNames(definitions []provider.ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Function.Name)
	}
	return names
}

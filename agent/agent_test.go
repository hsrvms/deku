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

	"github.com/hsrvms/deku/approval"
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
	if got := toolNames(providerStub.tools); !reflect.DeepEqual(got, []string{"command", "edit", "grep", "ls", "read", "write"}) {
		t.Errorf("tools = %#v, want command, edit, grep, ls, read, write", got)
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
	if len(providerStub.requests[0].tools) != 6 || len(providerStub.requests[1].tools) != 6 {
		t.Fatalf("tool definitions per step = %d and %d, want 6 and 6", len(providerStub.requests[0].tools), len(providerStub.requests[1].tools))
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
	if !strings.Contains(output.String(), "Tool output (edit, write):") || !strings.Contains(output.String(), "tool error:") {
		t.Errorf("output = %q, want failed edit tool output echoed to the terminal", output.String())
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
	if !strings.Contains(output.String(), "Tool output (edit, write):") || !strings.Contains(output.String(), "  Applied 1 replacement(s) to main.go.") {
		t.Errorf("output = %q, want edit tool output echoed to the terminal", output.String())
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
	if !strings.Contains(output.String(), "Rejected the edit tool call; it did not execute.") {
		t.Errorf("output = %q, want rejection notice shown to the user", output.String())
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || !strings.Contains(toolResult.Content, "rejected the edit tool call") {
		t.Errorf("rejected tool result = %#v, want denial reported to model", toolResult)
	}
}

func TestAgentGatesWriteToolBehindApprovalAndCreatesFile(t *testing.T) {
	root := t.TempDir()
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
			{provider.ToolCall{ID: "call-1", Name: "write", Arguments: `{"path":"notes.txt","content":"hello\n"}`}, provider.Done{}},
			{provider.TextDelta{Text: "Created the file."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader("y\n"), registry)

	result, err := runner.Turn(context.Background(), "Create notes.txt.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "Created the file." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	if !strings.Contains(output.String(), "Approve?") {
		t.Errorf("output = %q, want approval prompt", output.String())
	}
	got, err := os.ReadFile(filepath.Join(root, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Errorf("written file = %q, want supplied content", got)
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || toolResult.ToolCallID != "call-1" {
		t.Errorf("tool result message = %#v", toolResult)
	}
	if toolResult.Content != "Wrote notes.txt." {
		t.Errorf("approved write tool result = %q", toolResult.Content)
	}
}

func TestAgentReportsWriteToolDenialOnRejection(t *testing.T) {
	root := t.TempDir()
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
			{provider.ToolCall{ID: "call-1", Name: "write", Arguments: `{"path":"notes.txt","content":"hello\n"}`}, provider.Done{}},
			{provider.TextDelta{Text: "The write was declined."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader("n\n"), registry)

	result, err := runner.Turn(context.Background(), "Create notes.txt.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "The write was declined." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("rejected write created a file: %v", err)
	}
	if !strings.Contains(output.String(), "Rejected the write tool call; it did not execute.") {
		t.Errorf("output = %q, want rejection notice shown to the user", output.String())
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || !strings.Contains(toolResult.Content, "rejected the write tool call") {
		t.Errorf("rejected tool result = %#v, want denial reported to model", toolResult)
	}
}

func TestAgentExecutesApprovedCommandToolCall(t *testing.T) {
	root := t.TempDir()
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
			{provider.ToolCall{ID: "call-1", Name: "command", Arguments: `{"command":"echo hello"}`}, provider.Done{}},
			{provider.TextDelta{Text: "Ran the command."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader("y\n"), registry)

	result, err := runner.Turn(context.Background(), "Say hello.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "Ran the command." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	if !strings.Contains(output.String(), "WARNING") || !strings.Contains(output.String(), "Approve?") {
		t.Errorf("output = %q, want destructive warning and approval prompt", output.String())
	}
	if !strings.Contains(output.String(), "Tool output (command, destructive):") || !strings.Contains(output.String(), "  exit code: 0") || !strings.Contains(output.String(), "  hello") {
		t.Errorf("output = %q, want command tool output echoed to the terminal", output.String())
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || toolResult.ToolCallID != "call-1" {
		t.Errorf("tool result message = %#v", toolResult)
	}
	if !strings.Contains(toolResult.Content, "exit code: 0") || !strings.Contains(toolResult.Content, "hello") {
		t.Errorf("command tool result = %q, want exit code and captured output", toolResult.Content)
	}
}

func TestAgentRejectsCommandToolCallOnDenial(t *testing.T) {
	root := t.TempDir()
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
			{provider.ToolCall{ID: "call-1", Name: "command", Arguments: `{"command":"touch side_effect.txt"}`}, provider.Done{}},
			{provider.TextDelta{Text: "The command was declined."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader("n\n"), registry)

	result, err := runner.Turn(context.Background(), "Run a command.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "The command was declined." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	if _, err := os.Stat(filepath.Join(root, "side_effect.txt")); !os.IsNotExist(err) {
		t.Fatalf("rejected command created a side effect: %v", err)
	}
	if !strings.Contains(output.String(), "Rejected the command tool call; it did not execute.") {
		t.Errorf("output = %q, want rejection notice shown to the user", output.String())
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || !strings.Contains(toolResult.Content, "rejected the command tool call") {
		t.Errorf("rejected tool result = %#v, want denial reported to model", toolResult)
	}
}

func TestAgentShowsCommandReportBeforeApprovalDecision(t *testing.T) {
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
	rendered := output.String()
	if !strings.Contains(rendered, "Command Report:") || !strings.Contains(rendered, "- func main() {}") || !strings.Contains(rendered, "+ func run() {}") {
		t.Errorf("output = %q, want Command Report shown at the Approval point", rendered)
	}
	if strings.Index(rendered, "- func main() {}") >= strings.Index(rendered, "Approve?") {
		t.Errorf("output = %q, want Command Report before the y/n decision", rendered)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc run() {}\n" {
		t.Errorf("file after approved edit = %q, want func run()", got)
	}
}

func TestAgentAutoApprovesReadToolWhileShowingReport(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o600); err != nil {
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
			{provider.TextDelta{Text: "Read it."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader(""), registry)

	result, err := runner.Turn(context.Background(), "Read main.go.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "Read it." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	rendered := output.String()
	if !strings.Contains(rendered, "Command Report:") || !strings.Contains(rendered, "Read: main.go") {
		t.Errorf("output = %q, want Read Command Report shown", rendered)
	}
	if strings.Contains(rendered, "Approve?") {
		t.Errorf("output = %q, want no y/n prompt for an auto-approved Read Tool", rendered)
	}
	if !strings.Contains(rendered, "Tool output (read, read):") || !strings.Contains(rendered, "  package main") {
		t.Errorf("output = %q, want tool output echoed for the auto-approved Read Tool", rendered)
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || toolResult.Content != "package main\n" {
		t.Errorf("read tool result = %#v, want file content", toolResult)
	}
}

func TestAgentRefusesToolCallWithoutRenderableReport(t *testing.T) {
	root := t.TempDir()
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
			{provider.ToolCall{ID: "call-1", Name: "write", Arguments: `{"content":"hello"}`}, provider.Done{}},
			{provider.TextDelta{Text: "The write was refused."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader("y\n"), registry)

	result, err := runner.Turn(context.Background(), "Write a file.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "The write was refused." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	if strings.Contains(output.String(), "Approve?") {
		t.Errorf("output = %q, want no approval prompt for a refused call", output.String())
	}
	if !strings.Contains(output.String(), "Refused the write tool call; its Command Report could not be rendered.") {
		t.Errorf("output = %q, want a refusal notice with the same visibility as a rejection", output.String())
	}
	if !strings.Contains(output.String(), "Tool output (write, write):") || !strings.Contains(output.String(), "tool error:") {
		t.Errorf("output = %q, want the refused content echoed like executed tool output", output.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("refused write created files: %v", entries)
	}
	toolResult := conversation.Messages[2]
	if toolResult.Role != session.RoleTool || !strings.Contains(toolResult.Content, "tool error") {
		t.Errorf("refused tool result = %#v, want refusal reported to the model", toolResult)
	}
}

func TestAgentInjectsFreshRepositoryMapIntoEveryStep(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
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
			{provider.ToolCall{ID: "call-1", Name: "write", Arguments: `{"path":"notes.txt","content":"hello\n"}`}, provider.Done{}},
			{provider.TextDelta{Text: "Created notes.txt."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithTools(providerStub, "test-model", conversation, &output, strings.NewReader("y\n"), registry)

	result, err := runner.Turn(context.Background(), "Add a notes file.")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "Created notes.txt." {
		t.Errorf("response = %q, want final response", result.Response)
	}
	if len(providerStub.requests) != 2 {
		t.Fatalf("provider steps = %d, want 2", len(providerStub.requests))
	}
	first := providerStub.requests[0].system
	second := providerStub.requests[1].system
	for i, system := range []string{first, second} {
		if !strings.Contains(system, "The map shows file paths, not source code.") {
			t.Errorf("step %d system prompt lacks the not-source-code instruction: %q", i+1, system)
		}
		if !strings.Contains(system, "Use `read` to obtain file contents before editing.") {
			t.Errorf("step %d system prompt lacks the use-read instruction: %q", i+1, system)
		}
		if !strings.Contains(system, "main.go") {
			t.Errorf("step %d system prompt does not include main.go in the map: %q", i+1, system)
		}
	}
	if strings.Contains(first, "notes.txt") {
		t.Errorf("first-step map %q already shows notes.txt before it exists", first)
	}
	if !strings.Contains(second, "notes.txt") {
		t.Errorf("second-step map %q did not refresh to include the newly written notes.txt", second)
	}
}

func TestAgentRepositoryMapHonorsExclusionPolicy(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scratch.tmp"), []byte("x"), 0o600); err != nil {
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
			{provider.TextDelta{Text: "Inspecting the repository."}, provider.Done{}},
		},
	}
	var output bytes.Buffer
	runner := NewWithPolicy(providerStub, "test-model", conversation, &output, nil, registry, approval.DefaultPolicy(), []string{"*.tmp"})

	if _, err := runner.Turn(context.Background(), "Inspect the repository."); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	system := providerStub.requests[0].system
	if !strings.Contains(system, "main.go") {
		t.Errorf("system prompt %q does not include main.go", system)
	}
	if strings.Contains(system, "scratch.tmp") {
		t.Errorf("system prompt %q includes scratch.tmp excluded by policy", system)
	}
}

type continuationProvider struct {
	responses [][]provider.Event
	requests  []providerRequest
	calls     int
}

type providerRequest struct {
	system   string
	messages []provider.Message
	tools    []provider.ToolDefinition
}

func (p *continuationProvider) Chat(_ context.Context, _ string, system string, messages []provider.Message, tools []provider.ToolDefinition) (<-chan provider.Event, error) {
	if p.calls >= len(p.responses) {
		return nil, errors.New("unexpected provider call")
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

func toolNames(definitions []provider.ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Function.Name)
	}
	return names
}

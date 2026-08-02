package agent

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/session"
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
	agent := New(providerStub, "test-model", conversation, &output)

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
	if len(providerStub.tools) != 0 {
		t.Errorf("tools = %#v, want no tools", providerStub.tools)
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

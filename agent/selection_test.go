package agent

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/repository"
	"github.com/hsrvms/deku/session"
	"github.com/hsrvms/deku/tool"
)

// newSelectionTools returns a Tool registry rooted at a fresh temporary
// directory, so Selection tests run real Turns with the built-in tools.
func newSelectionTools(t *testing.T) *tool.Registry {
	t.Helper()
	registry, err := tool.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

// scriptedSelectionSource is the Agent-seam fake for Selection resolution:
// each known Selection maps to its own scripted Adapter, and an unknown or
// empty Selection fails like the production Registry would.
type scriptedSelectionSource struct {
	adapters map[provider.Selection]*scriptedProvider
}

func (s *scriptedSelectionSource) Resolve(selection provider.Selection) (provider.Chat, error) {
	if selection.IsZero() {
		return nil, fmt.Errorf("no Provider or Model is selected")
	}
	adapter, ok := s.adapters[selection]
	if !ok {
		return nil, fmt.Errorf("unknown selection %+v", selection)
	}
	return adapter, nil
}

// respondingProvider returns a scripted Adapter that answers with text.
func respondingProvider(text string) *scriptedProvider {
	return &scriptedProvider{events: []provider.Event{
		provider.TextDelta{Text: text},
		provider.Done{},
	}}
}

func TestNewWithSelectionUsesResolvedAdapter(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	tools := newSelectionTools(t)
	selection := provider.Selection{Provider: "first", Model: "model-a"}
	first := respondingProvider("from first")
	source := &scriptedSelectionSource{adapters: map[provider.Selection]*scriptedProvider{selection: first}}

	var output bytes.Buffer
	agent, err := NewWithSelection(source, selection, conversation, &output, nil, tools, approval.DefaultPolicy(), nil, nil, repository.ModeOff, "")
	if err != nil {
		t.Fatalf("NewWithSelection() error = %v", err)
	}

	result, err := agent.Turn(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "from first" {
		t.Errorf("response = %q, want the resolved adapter's response", result.Response)
	}
	if first.model != "model-a" {
		t.Errorf("model = %q, want the Selection's model", first.model)
	}
}

func TestNewWithSelectionInvalidSelectionFails(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	tools := newSelectionTools(t)
	source := &scriptedSelectionSource{adapters: map[provider.Selection]*scriptedProvider{}}

	_, err = NewWithSelection(source, provider.Selection{Provider: "ghost", Model: "model"}, conversation, nil, nil, tools, approval.DefaultPolicy(), nil, nil, repository.ModeOff, "")
	if err == nil {
		t.Fatal("expected error for a selection the source cannot resolve")
	}
}

func TestSetSelectionSwitchesAdapterBetweenTurns(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	tools := newSelectionTools(t)
	firstSel := provider.Selection{Provider: "first", Model: "model-a"}
	secondSel := provider.Selection{Provider: "second", Model: "model-b"}
	first := respondingProvider("from first")
	second := respondingProvider("from second")
	source := &scriptedSelectionSource{adapters: map[provider.Selection]*scriptedProvider{
		firstSel:  first,
		secondSel: second,
	}}

	var output bytes.Buffer
	agent, err := NewWithSelection(source, firstSel, conversation, &output, nil, tools, approval.DefaultPolicy(), nil, nil, repository.ModeOff, "")
	if err != nil {
		t.Fatalf("NewWithSelection() error = %v", err)
	}

	if _, err := agent.Turn(context.Background(), "first request"); err != nil {
		t.Fatalf("first Turn() error = %v", err)
	}
	if err := agent.SetSelection(secondSel); err != nil {
		t.Fatalf("SetSelection() error = %v", err)
	}
	result, err := agent.Turn(context.Background(), "second request")
	if err != nil {
		t.Fatalf("second Turn() error = %v", err)
	}

	if result.Response != "from second" {
		t.Errorf("response = %q, want the new Selection's adapter", result.Response)
	}
	if second.model != "model-b" {
		t.Errorf("second adapter model = %q, want the new Selection's model", second.model)
	}
	if first.model != "model-a" {
		t.Errorf("first adapter model = %q, want it untouched by the switch", first.model)
	}
}

func TestSetSelectionRecordsOverrideInSession(t *testing.T) {
	dir := t.TempDir()
	store, err := session.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	tools := newSelectionTools(t)
	firstSel := provider.Selection{Provider: "first", Model: "model-a"}
	secondSel := provider.Selection{Provider: "second", Model: "model-b"}
	first := respondingProvider("from first")
	second := respondingProvider("from second")
	source := &scriptedSelectionSource{adapters: map[provider.Selection]*scriptedProvider{
		firstSel:  first,
		secondSel: second,
	}}

	agent, err := NewWithSelection(source, firstSel, conversation, nil, nil, tools, approval.DefaultPolicy(), nil, nil, repository.ModeOff, "")
	if err != nil {
		t.Fatalf("NewWithSelection() error = %v", err)
	}
	if err := agent.SetSelection(secondSel); err != nil {
		t.Fatalf("SetSelection() error = %v", err)
	}

	// The override is recorded in the Session transcript and restored on
	// resume, so a new Agent for the resumed Session runs the override.
	resumed, err := store.Resume(conversation.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	got, ok := resumed.LatestSelection()
	if !ok {
		t.Fatal("resumed session has no recorded Selection override")
	}
	if got != (provider.Selection{Provider: "second", Model: "model-b"}) {
		t.Errorf("resumed override = %#v, want the recorded Selection", got)
	}

	resumedAgent, err := NewWithSelection(source, provider.Selection{Provider: got.Provider, Model: got.Model}, resumed, nil, nil, tools, approval.DefaultPolicy(), nil, nil, repository.ModeOff, "")
	if err != nil {
		t.Fatalf("NewWithSelection() for the resumed session error = %v", err)
	}
	result, err := resumedAgent.Turn(context.Background(), "after resume")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "from second" {
		t.Errorf("response = %q, want the restored override to apply", result.Response)
	}
}

func TestSetSelectionFailureKeepsActiveSelection(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	tools := newSelectionTools(t)
	firstSel := provider.Selection{Provider: "first", Model: "model-a"}
	first := respondingProvider("from first")
	source := &scriptedSelectionSource{adapters: map[provider.Selection]*scriptedProvider{firstSel: first}}

	agent, err := NewWithSelection(source, firstSel, conversation, nil, nil, tools, approval.DefaultPolicy(), nil, nil, repository.ModeOff, "")
	if err != nil {
		t.Fatalf("NewWithSelection() error = %v", err)
	}

	err = agent.SetSelection(provider.Selection{Provider: "ghost", Model: "model"})
	if err == nil {
		t.Fatal("expected error for a selection the source cannot resolve")
	}
	if _, ok := conversation.LatestSelection(); ok {
		t.Error("failed SetSelection recorded an override, want none")
	}

	// The active Selection is unchanged: the next Turn still runs the
	// original adapter.
	result, err := agent.Turn(context.Background(), "still first")
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if result.Response != "from first" {
		t.Errorf("response = %q, want the original Selection after a failed switch", result.Response)
	}
}

func TestSetSelectionWithoutSelectionSourceFails(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	agent := New(respondingProvider("fixed"), "fixed-model", conversation, nil, nil)

	err = agent.SetSelection(provider.Selection{Provider: "other", Model: "model"})
	if err == nil {
		t.Fatal("expected error when the agent has no Selection source")
	}
}

func TestNewWithSelectionRequiresSource(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	tools := newSelectionTools(t)
	_, err = NewWithSelection(nil, provider.Selection{Provider: "p", Model: "m"}, conversation, nil, nil, tools, approval.DefaultPolicy(), nil, nil, repository.ModeOff, "")
	if err == nil {
		t.Fatal("expected error for a nil Selection source")
	}
}

func TestNewWithSelectionSelectionsAreDistinctPerTurn(t *testing.T) {
	// A Selection change between Turns must not rewrite history: both Turns
	// stay in the transcript in order, with the override recorded between
	// them.
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	tools := newSelectionTools(t)
	firstSel := provider.Selection{Provider: "first", Model: "model-a"}
	secondSel := provider.Selection{Provider: "second", Model: "model-b"}
	source := &scriptedSelectionSource{adapters: map[provider.Selection]*scriptedProvider{
		firstSel:  respondingProvider("first answer"),
		secondSel: respondingProvider("second answer"),
	}}

	agent, err := NewWithSelection(source, firstSel, conversation, nil, nil, tools, approval.DefaultPolicy(), nil, nil, repository.ModeOff, "")
	if err != nil {
		t.Fatalf("NewWithSelection() error = %v", err)
	}
	if _, err := agent.Turn(context.Background(), "one"); err != nil {
		t.Fatalf("first Turn() error = %v", err)
	}
	if err := agent.SetSelection(secondSel); err != nil {
		t.Fatalf("SetSelection() error = %v", err)
	}
	if _, err := agent.Turn(context.Background(), "two"); err != nil {
		t.Fatalf("second Turn() error = %v", err)
	}

	var roles []string
	for _, message := range conversation.Messages {
		roles = append(roles, message.Role)
	}
	want := []string{
		session.RoleUser, session.RoleAssistant,
		session.RoleUser, session.RoleAssistant,
	}
	if !reflect.DeepEqual(roles, want) {
		t.Errorf("transcript roles = %#v, want %#v", roles, want)
	}
}

package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hsrvms/deku/activity"
	"github.com/hsrvms/deku/agent"
	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/repository"
	"github.com/hsrvms/deku/session"
	"github.com/hsrvms/deku/tool"
)

// stubRunner records Turn requests and returns a scripted result, so the
// shell is exercised without a Provider.
type stubRunner struct {
	requests []string
	result   agent.TurnResult
	err      error
}

func (s *stubRunner) Turn(_ context.Context, request string) (agent.TurnResult, error) {
	s.requests = append(s.requests, request)
	return s.result, s.err
}

func newTestModel(approval io.Writer) *Model {
	return New("tokenrouter", "qwen-2.5-coder", approval)
}

func typeText(m *Model, text string) {
	for _, r := range text {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func enterKey() tea.Msg  { return tea.KeyMsg{Type: tea.KeyEnter} }
func ctrlC() tea.Msg     { return tea.KeyMsg{Type: tea.KeyCtrlC} }
func ctrlD() tea.Msg     { return tea.KeyMsg{Type: tea.KeyCtrlD} }
func pgUp() tea.Msg      { return tea.KeyMsg{Type: tea.KeyPgUp} }
func wheelUp() tea.Msg   { return tea.MouseMsg{Button: tea.MouseButtonWheelUp} }
func wheelDown() tea.Msg { return tea.MouseMsg{Button: tea.MouseButtonWheelDown} }

// completeTurn runs a pending Turn command (the tea.Cmd returned by Enter)
// and feeds its message to the model, as the program loop would.
func completeTurn(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a Turn command, got nil")
	}
	m.Update(cmd())
}

func TestActive(t *testing.T) {
	tests := []struct {
		name     string
		terminal bool
		term     string
		noColor  string
		want     bool
	}{
		{"tty", true, "xterm-256color", "", true},
		{"pipe", false, "xterm-256color", "", false},
		{"non-tty file", false, "xterm", "", false},
		{"dumb term", true, "dumb", "", false},
		{"dumb term case-insensitive", true, "DUMB", "", false},
		{"no color", true, "xterm-256color", "1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Active(tt.terminal, tt.term, tt.noColor); got != tt.want {
				t.Errorf("Active(%v, %q, %q) = %v, want %v", tt.terminal, tt.term, tt.noColor, got, tt.want)
			}
		})
	}
}

func TestViewLayoutDefaultsBeforeFirstResize(t *testing.T) {
	m := newTestModel(io.Discard)
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 24 {
		t.Fatalf("default View height = %d lines, want 24", len(lines))
	}
	if !strings.Contains(lines[22], "tokenrouter/qwen-2.5-coder") {
		t.Errorf("status bar line = %q, want the Provider/Model", lines[22])
	}
	if !strings.HasPrefix(lines[23], "> ") {
		t.Errorf("input line = %q, want the prompt prefix", lines[23])
	}
}

func TestEnterSubmitsTurnAndRendersResult(t *testing.T) {
	runner := &stubRunner{result: agent.TurnResult{
		Validation: &agent.ValidationResult{Command: "go test ./...", Passed: true},
	}}
	m := newTestModel(io.Discard)
	m.SetRunner(runner)

	typeText(m, "explain this")
	_, cmd := m.Update(enterKey())
	if cmd == nil {
		t.Fatal("Enter on an idle shell must start a Turn")
	}
	if view := m.View(); !strings.Contains(view, "> explain this") {
		t.Errorf("Transcript must echo the submitted request, got %q", view)
	}
	if !m.turnActive {
		t.Error("Turn must be active after submission")
	}

	completeTurn(t, m, cmd)
	if len(runner.requests) != 1 || runner.requests[0] != "explain this" {
		t.Errorf("runner requests = %#v, want [explain this]", runner.requests)
	}
	if m.turnActive {
		t.Error("Turn must finish when the result message arrives")
	}
	if view := m.View(); !strings.Contains(view, "deku: validation passed (go test ./...)") {
		t.Errorf("Transcript must report the Validation outcome, got %q", view)
	}
}

func TestTurnErrorIsReported(t *testing.T) {
	runner := &stubRunner{err: errors.New("provider stream: boom")}
	m := newTestModel(io.Discard)
	m.SetRunner(runner)

	typeText(m, "hello")
	_, cmd := m.Update(enterKey())
	completeTurn(t, m, cmd)

	if m.turnActive {
		t.Error("Turn must finish on error")
	}
	if view := m.View(); !strings.Contains(view, "deku: provider stream: boom") {
		t.Errorf("Transcript must report the Turn error, got %q", view)
	}
}

func TestTranscriptStreamsOutputIncrementally(t *testing.T) {
	m := newTestModel(io.Discard)
	if _, err := m.Write([]byte("Hello, ")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if view := m.View(); !strings.Contains(view, "Hello,") {
		t.Errorf("Transcript must show the first chunk, got %q", view)
	}
	if _, err := m.Write([]byte("world!")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if view := m.View(); !strings.Contains(view, "Hello, world!") {
		t.Errorf("Transcript must accumulate chunks, got %q", view)
	}
}

func TestStatusBarShowsIndicatorToolAndSelection(t *testing.T) {
	m := newTestModel(io.Discard)

	// Idle: no indicator claim, the Selection is always visible.
	if view := m.View(); !strings.Contains(view, "tokenrouter/qwen-2.5-coder") {
		t.Errorf("status bar must name the Provider/Model, got %q", view)
	}

	m.Indicator(activity.Thinking)
	if view := m.View(); !strings.Contains(view, "● thinking") {
		t.Errorf("Thinking indicator missing, got %q", view)
	}
	m.ActiveTool("read")
	if view := m.View(); !strings.Contains(view, "tool: read") {
		t.Errorf("active Tool missing, got %q", view)
	}
	m.Indicator(activity.Working)
	if view := m.View(); !strings.Contains(view, "▶ working") {
		t.Errorf("Working indicator missing, got %q", view)
	}
	m.Indicator(activity.AwaitingApproval)
	if view := m.View(); !strings.Contains(view, "? awaiting approval") {
		t.Errorf("Awaiting-Approval indicator missing, got %q", view)
	}
}

func TestChangeEventsAreRecordedInStreamOrder(t *testing.T) {
	m := newTestModel(io.Discard)
	m.Change(activity.Change{Tool: "edit", Path: "main.go"})
	m.Change(activity.Change{Tool: "write", Path: "notes.txt"})

	want := []activity.Change{
		{Tool: "edit", Path: "main.go"},
		{Tool: "write", Path: "notes.txt"},
	}
	if got := m.Changes(); !reflect.DeepEqual(got, want) {
		t.Errorf("Changes() = %#v, want %#v", got, want)
	}
}

func TestEnterWhileTurnRunsDoesNotStartAnother(t *testing.T) {
	runner := &stubRunner{}
	m := newTestModel(io.Discard)
	m.SetRunner(runner)

	typeText(m, "first")
	_, cmd := m.Update(enterKey())
	_ = cmd // Turn stays pending for the whole test

	typeText(m, "second")
	_, cmd = m.Update(enterKey())
	if cmd != nil {
		t.Fatal("Enter while a Turn runs must not start another Turn")
	}
	// Typing still works: the line keeps the text (queuing arrives later).
	if view := m.View(); !strings.Contains(view, "second") {
		t.Errorf("typed text must remain in the input line, got %q", view)
	}
}

func TestEnterRoutesApprovalDecisionToTheGate(t *testing.T) {
	var decisions bytes.Buffer
	m := newTestModel(&decisions)
	m.SetRunner(&stubRunner{})

	typeText(m, "create notes")
	_, turnCmd := m.Update(enterKey())
	_ = turnCmd // the Turn blocks on Approval until the decision arrives

	m.Indicator(activity.AwaitingApproval)
	typeText(m, "y")
	_, cmd := m.Update(enterKey())
	if cmd != nil {
		t.Fatal("Enter during Approval must not start a new Turn")
	}
	if got := decisions.String(); got != "y\n" {
		t.Errorf("Approval decision = %q, want %q", got, "y\n")
	}
	if view := m.View(); strings.Contains(view, "y█") {
		t.Errorf("input line must clear after the decision, got %q", view)
	}
}

func TestCommandDispatchUpdatesSelectionDisplay(t *testing.T) {
	m := newTestModel(io.Discard)
	m.SetCommands(func(line string) (string, string, string, error) {
		if line != "/model other other-model" {
			t.Errorf("command line = %q, want /model other other-model", line)
		}
		return "selection: other / other-model\n", "other", "other-model", nil
	})

	typeText(m, "/model other other-model")
	_, cmd := m.Update(enterKey())
	if cmd != nil {
		t.Fatal("a command must not start a Turn")
	}
	if view := m.View(); !strings.Contains(view, "selection: other / other-model") {
		t.Errorf("Transcript must show the command reply, got %q", view)
	}
	if view := m.View(); !strings.Contains(view, "other/other-model") {
		t.Errorf("status bar must update to the new Selection, got %q", view)
	}
}

func TestCommandErrorIsReported(t *testing.T) {
	m := newTestModel(io.Discard)
	m.SetCommands(func(string) (string, string, string, error) {
		return "", "", "", errors.New("usage: /model [provider model]")
	})

	typeText(m, "/model")
	_, cmd := m.Update(enterKey())
	if cmd != nil {
		t.Fatal("a failing command must not start a Turn")
	}
	if view := m.View(); !strings.Contains(view, "deku: usage: /model [provider model]") {
		t.Errorf("Transcript must report the command error, got %q", view)
	}
}

func TestUnknownCommandWithoutHandlerIsReported(t *testing.T) {
	m := newTestModel(io.Discard)
	typeText(m, "/model")
	_, cmd := m.Update(enterKey())
	if cmd != nil {
		t.Fatal("an unhandled command must not start a Turn")
	}
	if view := m.View(); !strings.Contains(view, "deku: unknown command") {
		t.Errorf("Transcript must report the unknown command, got %q", view)
	}
}

func TestCtrlCAndCtrlDQuit(t *testing.T) {
	for _, key := range []tea.Msg{ctrlC(), ctrlD()} {
		m := newTestModel(io.Discard)
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("key %v must quit the program", key)
		}
		if msg := cmd(); msg != tea.Quit() {
			t.Errorf("key %v: cmd() = %#v, want the quit message", key, msg)
		}
	}
}

func TestTranscriptScrollsAndFollowsContent(t *testing.T) {
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12}) // pane height 10

	var content strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&content, "line %02d\n", i)
	}
	if _, err := m.Write([]byte(content.String())); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// The view follows the newest content by default.
	if view := m.View(); !strings.Contains(view, "line 40") {
		t.Errorf("follow must pin the view to the newest content, got %q", view)
	}
	if view := m.View(); strings.Contains(view, "line 01") {
		t.Errorf("the pinned view must not show the top, got %q", view)
	}
	if p := m.ScrollPercent(); p != 1 {
		t.Errorf("ScrollPercent() = %v, want 1 when pinned", p)
	}

	// PageUp reveals earlier content (the trailing newline makes the
	// content 41 lines, so one page up from the bottom starts at line 22).
	m.Update(pgUp())
	if view := m.View(); !strings.Contains(view, "line 22") {
		t.Errorf("PageUp must reveal earlier content, got %q", view)
	}
	if view := m.View(); strings.Contains(view, "line 40") {
		t.Errorf("the scrolled view must not show the newest lines, got %q", view)
	}
	if p := m.ScrollPercent(); p >= 1 {
		t.Errorf("ScrollPercent() = %v, want < 1 after scrolling up", p)
	}

	// New content must not yank a reader who scrolled up.
	if _, err := m.Write([]byte("line 41\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if view := m.View(); strings.Contains(view, "line 41") {
		t.Errorf("new content must not move a scrolled-up view, got %q", view)
	}

	// Scrolling to the bottom re-pins the view.
	for range 4 {
		m.Update(wheelDown())
	}
	if view := m.View(); !strings.Contains(view, "line 41") {
		t.Errorf("scrolling to the bottom must show the newest content, got %q", view)
	}
	m.Update(wheelUp())
	if view := m.View(); strings.Contains(view, "line 41") {
		t.Errorf("a wheel tick up from the bottom must scroll away, got %q", view)
	}
}

func TestInputLineEditing(t *testing.T) {
	m := newTestModel(io.Discard)

	typeText(m, "abc")
	if view := m.View(); !strings.Contains(view, "abc█") {
		t.Errorf("typed text with cursor missing, got %q", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if view := m.View(); !strings.Contains(view, "ab█") {
		t.Errorf("backspace must delete before the cursor, got %q", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	typeText(m, "X")
	if view := m.View(); !strings.Contains(view, "aX█b") {
		t.Errorf("insert at the cursor missing, got %q", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	typeText(m, "!")
	if view := m.View(); !strings.Contains(view, "aXb!█") {
		t.Errorf("insert at the end missing, got %q", view)
	}
}

func TestFormatResult(t *testing.T) {
	tests := []struct {
		name   string
		result agent.TurnResult
		wants  []string
	}{
		{"stash validation commit", agent.TurnResult{
			StashRef:   "refs/stash@0",
			Validation: &agent.ValidationResult{Command: "go test ./...", Passed: true},
			CommitID:   "abc123",
		}, []string{
			"deku: stashed pre-existing work at refs/stash@0",
			"deku: validation passed (go test ./...)",
			"deku: agent commit created abc123",
		}},
		{"failed validation", agent.TurnResult{
			Validation: &agent.ValidationResult{Command: "go vet ./...", Passed: false},
		}, []string{"deku: validation failed (go vet ./...); work remains uncommitted"}},
		{"empty", agent.TurnResult{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatResult(tt.result)
			for _, want := range tt.wants {
				if !strings.Contains(got, want) {
					t.Errorf("formatResult() = %q, missing %q", got, want)
				}
			}
			if tt.wants == nil && got != "" {
				t.Errorf("formatResult() = %q, want empty", got)
			}
		})
	}
}

// scriptedProvider returns one canned event list per Chat call, like the
// agent package's fixture provider, so a real Agent can drive the Model.
type scriptedProvider struct {
	responses [][]provider.Event
	calls     int
}

func (p *scriptedProvider) Chat(_ context.Context, _ string, _ string, _ []provider.Message, _ []provider.ToolDefinition) (<-chan provider.Event, error) {
	events := make(chan provider.Event, len(p.responses[p.calls]))
	for _, event := range p.responses[p.calls] {
		events <- event
	}
	close(events)
	p.calls++
	return events, nil
}

func TestModelDrivesRealAgentTurn(t *testing.T) {
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
	providerStub := &scriptedProvider{
		responses: [][]provider.Event{
			{provider.TextDelta{Text: "Hello, "}, provider.TextDelta{Text: "world!"}, provider.Done{}},
		},
	}
	approvalReader, approvalWriter := io.Pipe()
	defer func() { _ = approvalReader.Close() }()
	m := New("tokenrouter", "qwen-2.5-coder", approvalWriter)
	runner := agent.NewWithActivity(providerStub, "qwen-2.5-coder", conversation, m, approvalReader, registry, approval.DefaultPolicy(), nil, m)
	m.SetRunner(runner)

	typeText(m, "hi")
	_, cmd := m.Update(enterKey())
	completeTurn(t, m, cmd)

	view := m.View()
	if !strings.Contains(view, "Hello, world!") {
		t.Errorf("Transcript must stream the provider text, got %q", view)
	}
	if m.indicator != activity.Thinking {
		t.Errorf("indicator = %q, want thinking after a completed Turn", m.indicator)
	}
	if len(m.Changes()) != 0 {
		t.Errorf("Changes() = %#v, want none for a text-only Turn", m.Changes())
	}
	messages := conversation.Messages
	if len(messages) != 2 || messages[0].Role != session.RoleUser || messages[1].Role != session.RoleAssistant {
		t.Errorf("session messages = %#v, want user then assistant", messages)
	}
}

// scriptedSource resolves every Selection to the scripted Provider, like the
// agent package's selection-test fixture.
type scriptedSource struct {
	adapter provider.Chat
}

func (s *scriptedSource) Resolve(provider.Selection) (provider.Chat, error) {
	return s.adapter, nil
}

// syncBuffer is a goroutine-safe bytes.Buffer: the program loop writes frames
// while the test polls them.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// gatedReader feeds keys in two phases: the request first, then — once the
// test has observed the streamed response — the Ctrl+C that quits, so the
// program loop is exercised end to end without timing races.
type gatedReader struct {
	first   []byte
	second  []byte
	release chan struct{}
	phase   int
}

func (g *gatedReader) Read(p []byte) (int, error) {
	switch g.phase {
	case 0:
		g.phase = 1
		return copy(p, g.first), nil
	case 1:
		<-g.release
		g.phase = 2
		return copy(p, g.second), nil
	default:
		return 0, io.EOF
	}
}

// TestProgramLoopStreamsRealAgentTurnEndToEnd runs the actual bubbletea
// program loop in memory — injected keys in, rendered frames out — with a
// real Agent and a scripted Provider, so the whole key → Turn → activity
// stream → frame path is exercised the way a user drives it.
func TestProgramLoopStreamsRealAgentTurnEndToEnd(t *testing.T) {
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
	providerStub := &scriptedProvider{
		responses: [][]provider.Event{
			{provider.TextDelta{Text: "hello from the model"}, provider.Done{}},
		},
	}
	approvalReader, approvalWriter := io.Pipe()
	defer func() { _ = approvalReader.Close() }()
	m := New("tokenrouter", "qwen-2.5-coder", approvalWriter)
	source := &scriptedSource{adapter: providerStub}
	runner, err := agent.NewWithSelectionAndActivity(source, provider.Selection{Provider: "tokenrouter", Model: "qwen-2.5-coder"}, conversation, m, approvalReader, registry, approval.DefaultPolicy(), nil, nil, repository.ModeOff, "", m)
	if err != nil {
		t.Fatalf("NewWithSelectionAndActivity() error = %v", err)
	}
	m.SetRunner(runner)

	keys := &gatedReader{
		first:   []byte("hello world\r"),
		second:  []byte{3}, // Ctrl+C quits
		release: make(chan struct{}),
	}
	var frames syncBuffer
	done := make(chan error, 1)
	go func() { done <- m.RunWith(tea.WithInput(keys), tea.WithOutput(&frames)) }()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(frames.String(), "hello from the model") {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the streamed response in the frames")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(keys.release)

	// The request echo and the status bar must be in the frames.
	if !strings.Contains(frames.String(), "> hello world") {
		t.Errorf("frames must echo the request, got %q", frames.String())
	}
	if !strings.Contains(frames.String(), "tokenrouter/qwen-2.5-coder") {
		t.Errorf("frames must show the status bar Selection, got %q", frames.String())
	}
	if err := <-done; err != nil {
		t.Fatalf("program: %v", err)
	}
	messages := conversation.Messages
	if len(messages) != 2 || messages[0].Role != session.RoleUser || messages[1].Role != session.RoleAssistant {
		t.Errorf("session messages = %#v, want user then assistant", messages)
	}
}

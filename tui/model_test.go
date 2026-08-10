package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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

// stripANSI removes SGR sequences so tests can assert rendered text without
// depending on ANSI details.
var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRegexp.ReplaceAllString(s, "") }

// userMessageLine returns the stripped Transcript line that carries the
// submitted request, or "" when it is not rendered.
func userMessageLine(view, request string) string {
	for _, line := range strings.Split(stripANSI(view), "\n") {
		if strings.HasSuffix(line, request) {
			return line
		}
	}
	return ""
}

// ruleLines counts the full-width separator lines in a rendered view.
func ruleLines(view string) int {
	count := 0
	for _, line := range strings.Split(stripANSI(view), "\n") {
		if line == strings.Repeat("─", 80) {
			count++
		}
	}
	return count
}

// completeTurn runs a pending Turn command (the tea.Cmd returned by Enter)
// and feeds its message to the model, as the program loop would. It returns
// the next Turn command the shell started, if any (a queued Turn).
func completeTurn(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a Turn command, got nil")
	}
	_, next := m.Update(cmd())
	return next
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
	// The request renders right-aligned in a semantic token color, with a
	// separator at the exchange boundary.
	want := strings.Repeat(" ", 80-len("explain this")) + "explain this"
	if line := userMessageLine(m.View(), "explain this"); line != want {
		t.Errorf("user message line = %q, want right-aligned %q", line, want)
	}
	if got := ruleLines(m.View()); got != 1 {
		t.Errorf("separator above the user message = %d, want 1", got)
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
	m.Indicator(activity.Idle)
	if view := m.View(); !strings.Contains(view, "● idle") {
		t.Errorf("Idle indicator missing, got %q", view)
	}
}

func TestToolOutputEventRendersDistinctBlock(t *testing.T) {
	m := newTestModel(io.Discard)
	m.ToolOutput(activity.ToolOutput{Name: "read", Tier: "read", Content: "package main\n"})

	// The Agent also writes the inline echo of the same block right after
	// the event; the pane must drop it so the block appears exactly once.
	if _, err := m.Write([]byte("Tool output (read, read):\n  package main\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	view := m.View()
	if got := strings.Count(view, "Tool output (read, read):"); got != 1 {
		t.Errorf("Tool Output block must render once, got %d", got)
	}
	if !strings.Contains(view, "package main") {
		t.Errorf("Tool Output content missing, got %q", view)
	}
	if got := ruleLines(view); got != 2 {
		t.Errorf("separators around the Tool Output block = %d, want 2", got)
	}
}

func TestCommandReportEventRendersDistinctBlock(t *testing.T) {
	m := newTestModel(io.Discard)
	m.CommandReport(activity.CommandReport{ToolName: "write", Tier: "write", Report: "Write: notes.txt"})

	// The Approval gate's inline prompt (report plus y/n question) is the
	// Write that follows the event; the pane drops it and keeps the block.
	if _, err := m.Write([]byte("The write tool is classified as write.\nCommand Report:\n  Write: notes.txt\nApprove? [y/n] ")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	view := m.View()
	if got := strings.Count(view, "Command Report (write, write):"); got != 1 {
		t.Errorf("Command Report block must render once, got %d", got)
	}
	if !strings.Contains(view, "Write: notes.txt") {
		t.Errorf("Report lines missing, got %q", view)
	}
	if strings.Contains(view, "Approve?") {
		t.Errorf("the inline approval prompt must be dropped, got %q", view)
	}
	if got := ruleLines(view); got != 2 {
		t.Errorf("separators around the Command Report block = %d, want 2", got)
	}
}

func TestSeparatorsFrameExchangesAndBlocksInSequence(t *testing.T) {
	m := newTestModel(io.Discard)
	m.SetRunner(&stubRunner{})

	typeText(m, "first")
	_, cmd := m.Update(enterKey())
	_ = cmd
	m.Indicator(activity.Idle)
	m.ToolOutput(activity.ToolOutput{Name: "read", Tier: "read", Content: "main.go"})
	if _, err := m.Write([]byte("Tool output (read, read):\n  main.go\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	// The response opens directly after the block's closing frame; its
	// separator must collapse into that frame, not add a second rule.
	if _, err := m.Write([]byte("inspected")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// One rule above the user message, two around the Tool Output block
	// (the block's closing rule doubles as the response opening).
	if got := ruleLines(m.View()); got != 3 {
		t.Errorf("separators = %d, want 3 (exchange boundary and block frame)", got)
	}
}

func TestConsecutiveStreamedChunksStayOneMessage(t *testing.T) {
	m := newTestModel(io.Discard)
	if _, err := m.Write([]byte("Hello, ")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := m.Write([]byte("world!")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if view := m.View(); !strings.Contains(view, "Hello, world!") {
		t.Errorf("Transcript must accumulate streamed chunks, got %q", view)
	}
	if got := ruleLines(m.View()); got != 0 {
		t.Errorf("streamed text must not add separators, got %d", got)
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

func TestQueuedMessagesDoNotInterleaveWithRunningTurn(t *testing.T) {
	runner := &stubRunner{}
	m := newTestModel(io.Discard)
	m.SetRunner(runner)

	typeText(m, "first")
	_, cmd := m.Update(enterKey())
	_ = cmd // Turn stays pending for the whole test

	typeText(m, "second")
	_, queued := m.Update(enterKey())
	if queued != nil {
		t.Fatal("Enter while a Turn runs must queue, not start another Turn")
	}
	// The queued message is committed: the input line is free again.
	if got := inputLineText(m); got != "> █" {
		t.Errorf("the input line must be free after queueing, got %q", got)
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

func TestCtrlCNoLongerQuits(t *testing.T) {
	m := newTestModel(io.Discard)
	if _, cmd := m.Update(ctrlC()); cmd != nil {
		t.Fatal("Ctrl+C must interrupt or clear, never quit")
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
	if m.indicator != activity.Idle {
		t.Errorf("indicator = %q, want idle after a completed Turn", m.indicator)
	}
	if view := m.View(); !strings.Contains(view, "● idle") {
		t.Errorf("status bar must show the idle indicator, got %q", view)
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

// phasedReader feeds key phases in order, gating each phase until the test
// has observed the program state that phase answers, so the program loop is
// exercised end to end without timing races. A nil gate delivers immediately.
type phasedReader struct {
	phases [][]byte
	gates  []chan struct{}
	phase  int
}

func (p *phasedReader) Read(buf []byte) (int, error) {
	if p.phase >= len(p.phases) {
		return 0, io.EOF
	}
	if gate := p.gates[p.phase]; gate != nil {
		<-gate
	}
	data := p.phases[p.phase]
	p.phase++
	return copy(buf, data), nil
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

	keys := &phasedReader{
		phases: [][]byte{[]byte("hello world\r"), {4}}, // Ctrl+D quits
		gates:  []chan struct{}{nil, make(chan struct{})},
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
	close(keys.gates[1])

	// The request echo and the status bar must be in the frames.
	if !strings.Contains(frames.String(), "hello world") {
		t.Errorf("frames must show the submitted request, got %q", frames.String())
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

// TestProgramLoopTypedApprovalDecisionEndToEnd runs the actual bubbletea
// program loop with a real Agent and a gated Tool Call, typing the Approval
// decision into the input line: the prompt renders in the input area, the
// typed decision reaches the waiting gate through the pipe, the approved
// write executes, and the Turn completes to idle.
func TestProgramLoopTypedApprovalDecisionEndToEnd(t *testing.T) {
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
			{provider.ToolCall{ID: "call-1", Name: "write", Arguments: `{"path":"notes.txt","content":"hello\n"}`}, provider.Done{}},
			{provider.TextDelta{Text: "Created notes.txt."}, provider.Done{}},
		},
	}
	approvalReader, approvalWriter := io.Pipe()
	defer func() { _ = approvalReader.Close() }()
	m := New("tokenrouter", "qwen-2.5-coder", approvalWriter)
	runner := agent.NewWithActivity(providerStub, "qwen-2.5-coder", conversation, m, approvalReader, registry, approval.DefaultPolicy(), nil, m)
	m.SetRunner(runner)

	// Phase 0 submits the request; phase 1 types the Approval decision once
	// the prompt is visible; phase 2 quits after the Turn completes.
	keys := &phasedReader{
		phases: [][]byte{[]byte("create notes\r"), []byte("y\r"), {4}},
		gates:  []chan struct{}{nil, make(chan struct{}), make(chan struct{})},
	}
	var frames syncBuffer
	done := make(chan error, 1)
	go func() { done <- m.RunWith(tea.WithInput(keys), tea.WithOutput(&frames)) }()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(frames.String(), "Approve? [y/n]") {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the Approval prompt in the frames")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(keys.gates[1])
	for !strings.Contains(frames.String(), "● idle") {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the idle indicator after the Turn")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(keys.gates[2])
	if err := <-done; err != nil {
		t.Fatalf("program: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); err != nil {
		t.Fatalf("the typed Approval decision did not let the write execute: %v", err)
	}
	messages := conversation.Messages
	if len(messages) != 4 || messages[3].Role != session.RoleAssistant {
		t.Errorf("session messages = %#v, want user, assistant, tool result, assistant", messages)
	}
}

// TestRealAgentTurnRendersTypedBlocksEndToEnd runs a real Agent through the
// shell for a Turn that executes a gated Tool: the Command Report and Tool
// Output must render as typed blocks from the seam, the inline echo writes
// must be dropped, and the Agent's Idle indicator must end the Turn.
func TestRealAgentTurnRendersTypedBlocksEndToEnd(t *testing.T) {
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
			{provider.ToolCall{ID: "call-1", Name: "write", Arguments: `{"path":"notes.txt","content":"hello\n"}`}, provider.Done{}},
			{provider.TextDelta{Text: "Created notes.txt."}, provider.Done{}},
		},
	}
	approvalReader, approvalWriter := io.Pipe()
	m := New("tokenrouter", "qwen-2.5-coder", approvalWriter)
	runner := agent.NewWithActivity(providerStub, "qwen-2.5-coder", conversation, m, approvalReader, registry, approval.DefaultPolicy(), nil, m)
	m.SetRunner(runner)

	typeText(m, "create notes.txt")
	_, cmd := m.Update(enterKey())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = approvalWriter.Write([]byte("y\n")) // the gate blocks until this arrives
	}()
	completeTurn(t, m, cmd)
	<-done

	view := m.View()
	for _, want := range []string{
		"Command Report (write, write):",
		"Write: notes.txt",
		"Tool output (write, write):",
		"Wrote notes.txt.",
		"Created notes.txt.",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("Transcript must contain %q, got %q", want, view)
		}
	}
	// Each block renders once: the inline echo writes are dropped.
	if got := strings.Count(view, "Command Report (write, write):"); got != 1 {
		t.Errorf("Command Report block count = %d, want 1", got)
	}
	if got := strings.Count(view, "Tool output (write, write):"); got != 1 {
		t.Errorf("Tool Output block count = %d, want 1", got)
	}
	if strings.Contains(view, "Approve?") {
		t.Errorf("the inline approval prompt must be dropped, got %q", view)
	}
	if m.indicator != activity.Idle {
		t.Errorf("indicator = %q, want idle after the completed Turn", m.indicator)
	}
	if view := m.View(); !strings.Contains(view, "● idle") {
		t.Errorf("status bar must show the idle indicator, got %q", view)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); err != nil {
		t.Fatalf("approved write did not create the file: %v", err)
	}
}

func TestSeparatorAboveResponseAndNeverOnItsLastLine(t *testing.T) {
	m := newTestModel(io.Discard)
	m.SetRunner(&stubRunner{})

	typeText(m, "first")
	_, cmd := m.Update(enterKey())
	completeTurn(t, m, cmd)
	// Model text streams without a trailing newline, as the Agent writes it.
	if _, err := m.Write([]byte("done")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	// The next exchange's boundary separator must land under the response,
	// never on its last line.
	typeText(m, "second")
	_, cmd = m.Update(enterKey())
	_ = cmd

	view := m.View()
	// One separator above the first user request, one above its response,
	// one above the second user request.
	if got := ruleLines(view); got != 3 {
		t.Errorf("separators = %d, want 3", got)
	}
	for _, line := range strings.Split(stripANSI(view), "\n") {
		if strings.Contains(line, "done") && strings.Contains(line, "─") {
			t.Errorf("separator intercepts the response line %q", line)
		}
	}
	if line := userMessageLine(view, "second"); !strings.HasPrefix(strings.TrimSpace(line), "second") {
		t.Errorf("second user message missing, got %q", view)
	}
}

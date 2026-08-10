package tui

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hsrvms/deku/activity"
	"github.com/hsrvms/deku/agent"
)

// blockingRunner blocks until its Turn context is canceled, like a real
// Agent waiting on a Provider, so an interrupt can be observed. started
// receives one token per Turn.
type blockingRunner struct {
	started chan struct{}
}

func (b *blockingRunner) Turn(ctx context.Context, _ string) (agent.TurnResult, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return agent.TurnResult{}, ctx.Err()
}

func TestEnterWhileTurnRunsQueuesNextTurn(t *testing.T) {
	runner := &stubRunner{}
	m := newTestModel(io.Discard)
	m.SetRunner(runner)

	typeText(m, "first")
	_, cmd := m.Update(enterKey())
	if cmd == nil {
		t.Fatal("Enter on an idle shell must start a Turn")
	}

	// The input stays active while the Turn runs: typing works, and Enter
	// queues the message as the next Turn instead of starting a second one.
	typeText(m, "second")
	if _, queued := m.Update(enterKey()); queued != nil {
		t.Fatal("Enter while a Turn runs must queue, not start another Turn")
	}
	if view := m.View(); !strings.Contains(view, "second") {
		t.Errorf("the queued request must render as a user message, got %q", view)
	}
	if got := inputLineText(m); got != "> █" {
		t.Errorf("the input line must be free after queueing, got %q", got)
	}

	// Completing the running Turn starts the queued one automatically.
	next := completeTurn(t, m, cmd)
	if next == nil {
		t.Fatal("completing a Turn with a queued message must start the next Turn")
	}
	completeTurn(t, m, next)
	if len(runner.requests) != 2 || runner.requests[0] != "first" || runner.requests[1] != "second" {
		t.Errorf("runner requests = %#v, want [first second]", runner.requests)
	}
	if m.turnActive {
		t.Error("Turn must finish when the queue drains")
	}
}

func TestCtrlCInterruptsRunningTurn(t *testing.T) {
	runner := &blockingRunner{started: make(chan struct{}, 1)}
	m := newTestModel(io.Discard)
	m.SetRunner(runner)

	typeText(m, "hello")
	_, cmd := m.Update(enterKey())
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	<-runner.started

	// Typing still works while the Turn runs; Ctrl+C interrupts the Turn
	// and leaves the typed line alone.
	typeText(m, "and more")
	m.Update(ctrlC())
	if got := inputLineText(m); !strings.Contains(got, "and more") {
		t.Errorf("Ctrl+C during a Turn must not clear the typed line, got %q", got)
	}

	msg := <-done
	if _, next := m.Update(msg); next != nil {
		t.Fatal("an interrupted Turn must not start another Turn when nothing is queued")
	}
	if m.turnActive {
		t.Error("Turn must finish when interrupted")
	}
	if view := m.View(); !strings.Contains(view, "deku: turn interrupted") {
		t.Errorf("Transcript must report the interruption, got %q", view)
	}
}

func TestInterruptLeavesQueuedTurnsRunning(t *testing.T) {
	runner := &blockingRunner{started: make(chan struct{}, 2)}
	m := newTestModel(io.Discard)
	m.SetRunner(runner)

	typeText(m, "first")
	_, cmd := m.Update(enterKey())
	done := make(chan tea.Msg, 2)
	go func() { done <- cmd() }()
	<-runner.started

	typeText(m, "second")
	if _, queued := m.Update(enterKey()); queued != nil {
		t.Fatal("Enter while a Turn runs must queue")
	}

	// Interrupting the running Turn reports the interruption, then the
	// queued message runs as the next Turn.
	m.Update(ctrlC())
	msg := <-done
	_, next := m.Update(msg)
	if next == nil {
		t.Fatal("the queued Turn must start after the interrupted one")
	}
	if view := m.View(); !strings.Contains(view, "deku: turn interrupted") {
		t.Errorf("Transcript must report the interruption, got %q", view)
	}
	go func() { done <- next() }()
	<-runner.started
	m.Update(ctrlC())
	msg = <-done
	if _, next := m.Update(msg); next != nil {
		t.Fatal("no Turn may start after the queue drains")
	}
	if m.turnActive {
		t.Error("Turn must finish when the queue drains")
	}
}

func TestCtrlCWhenIdleClearsInput(t *testing.T) {
	m := newTestModel(io.Discard)
	typeText(m, "abc")
	if _, cmd := m.Update(ctrlC()); cmd != nil {
		t.Fatal("Ctrl+C when idle must not quit or start anything")
	}
	if got := inputLineText(m); got != "> █" {
		t.Errorf("Ctrl+C when idle must clear the input, got %q", got)
	}
	// The input returns to insert mode, so the next keystroke edits.
	typeText(m, "ok")
	if got := inputLineText(m); got != "> ok█" {
		t.Errorf("typing after Ctrl+C must edit the line, got %q", got)
	}
	// A second Ctrl+C with an empty line stays put.
	if _, cmd := m.Update(ctrlC()); cmd != nil {
		t.Fatal("Ctrl+C on an empty idle line must not quit")
	}
}

func TestCtrlDQuits(t *testing.T) {
	m := newTestModel(io.Discard)
	_, cmd := m.Update(ctrlD())
	if cmd == nil {
		t.Fatal("Ctrl+D must quit the program")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("Ctrl+D: cmd() = %#v, want the quit message", msg)
	}
}

func TestApprovalPromptRendersInInputArea(t *testing.T) {
	var decisions bytes.Buffer
	m := newTestModel(&decisions)
	m.SetRunner(&stubRunner{})

	typeText(m, "create notes")
	_, turnCmd := m.Update(enterKey())
	_ = turnCmd // the Turn blocks on Approval until the decision arrives

	m.Indicator(activity.AwaitingApproval)
	view := stripANSI(m.View())
	if !strings.Contains(view, "Approve? [y/n]") {
		t.Errorf("the Approval prompt must render in the input area, got %q", view)
	}
	if !strings.Contains(view, "? awaiting approval") {
		t.Errorf("the status bar must show awaiting Approval, got %q", view)
	}

	// The input stays a vim line: the decision is typed and Enter delivers
	// it to the waiting gate.
	m.Update(escKey())
	if got := inputLineText(m); !strings.Contains(got, "[normal] Approve? [y/n]") {
		t.Errorf("the Approval prompt must keep the mode tag, got %q", got)
	}
	m.Update(keyRune('i'))
	typeText(m, "y")
	if _, cmd := m.Update(enterKey()); cmd != nil {
		t.Fatal("Enter during Approval must not start a new Turn")
	}
	if got := decisions.String(); got != "y\n" {
		t.Errorf("Approval decision = %q, want %q", got, "y\n")
	}

	// An empty Enter during Approval delivers nothing, so the gate keeps
	// its prompt instead of re-prompting with an inline message.
	if _, cmd := m.Update(enterKey()); cmd != nil {
		t.Fatal("empty Enter during Approval must not start anything")
	}
	if decisions.String() != "y\n" {
		t.Errorf("empty Enter must not deliver a decision, got %q", decisions.String())
	}

	// The prompt leaves the input area when the Agent leaves the state.
	m.Indicator(activity.Idle)
	if view := stripANSI(m.View()); strings.Contains(view, "Approve?") {
		t.Errorf("the Approval prompt must leave the input area once approval ends, got %q", view)
	}
}

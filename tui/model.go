package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hsrvms/deku/activity"
	"github.com/hsrvms/deku/agent"
)

// Runner executes Turns for the shell. The *agent.Agent implements it; tests
// substitute a scripted runner so the shell is exercised without a Provider.
type Runner interface {
	Turn(ctx context.Context, request string) (agent.TurnResult, error)
}

// CommandHandler processes a "/" line before it becomes a Turn. It returns
// the reply to show in the Transcript and, when the command changed the
// Selection, the new Provider and Model names (empty when unchanged), so the
// status bar always names the current Provider/Model.
type CommandHandler func(line string) (reply string, provider, model string, err error)

// wheelScrollLines is how many Transcript lines one mouse-wheel tick scrolls.
const wheelScrollLines = 3

// Model is the bubbletea application model for the terminal UI. It is also
// the in-memory activity Sink and the Agent's output Writer: the Agent
// streams Working Indicator transitions, active-Tool reports, Change events,
// typed Tool Output and Command Report events, and model text straight into
// the panes, so the shell never infers Turn state. The Agent runs inside a
// tea.Cmd goroutine while Update and View run on the program loop; mu guards
// every field the two sides share.
type Model struct {
	runner   Runner
	commands CommandHandler
	approval io.Writer // decision lines for a pending Approval prompt
	provider string    // current Provider name for the status bar
	model    string    // current Model name for the status bar

	mu           sync.Mutex
	program      *tea.Program
	running      bool
	indicator    activity.Indicator
	activeTool   string
	changes      []activity.Change
	transcript   []transcriptEntry
	suppressEcho bool // the next Write is a typed block's inline echo; drop it
	viewport     viewport.Model
	follow       bool // pin the Transcript to the newest content

	repaint chan struct{} // coalesced redraw triggers for the forwarder

	width      int
	height     int
	input      input
	turnActive bool
}

// New constructs the terminal UI model. provider and model name the current
// Selection for the status bar; approval receives the user's decision lines
// for a pending Approval prompt (the Agent reads them from the other end of
// the pipe). The runner and command handler are attached afterwards with
// SetRunner and SetCommands, because the Agent is constructed with the Model
// as its output Writer and activity Sink.
func New(provider, model string, approval io.Writer) *Model {
	return &Model{
		provider: provider,
		model:    model,
		approval: approval,
		follow:   true,
		repaint:  make(chan struct{}, 1),
	}
}

// Run starts the bubbletea program: the alternate screen, mouse support for
// Transcript scrolling, and the key loop. It returns when the user quits.
func (m *Model) Run() error {
	return m.RunWith(tea.WithAltScreen(), tea.WithMouseCellMotion())
}

// RunWith starts the program with explicit options. Tests drive the full
// program loop in memory with tea.WithInput and tea.WithOutput; the
// production path goes through Run.
func (m *Model) RunWith(options ...tea.ProgramOption) error {
	m.mu.Lock()
	m.program = tea.NewProgram(m, options...)
	m.mu.Unlock()
	_, err := m.program.Run()
	return err
}

// SetRunner attaches the Turn runner. The Agent is constructed with the
// Model as its output Writer and activity Sink, so it can only be attached
// after New.
func (m *Model) SetRunner(runner Runner) { m.runner = runner }

// SetCommands installs the "/" line dispatch (the CLI's command handling).
func (m *Model) SetCommands(handler CommandHandler) { m.commands = handler }

// Append adds a shell notice to the Transcript pane (session notices, command
// replies, Turn reports) and schedules a repaint.
func (m *Model) Append(text string) {
	m.appendEntry(msgText, text)
	m.notify()
}

// Write implements io.Writer so the Agent's streamed output lands in the
// Transcript pane as it arrives. The Write immediately following a typed Tool
// Output or Command Report event carries that block's inline echo, which the
// pane already rendered from the seam; it is dropped so the block appears
// once and the inline renderer's plain text never leaks into the pane.
func (m *Model) Write(p []byte) (int, error) {
	m.mu.Lock()
	suppress := m.suppressEcho
	m.suppressEcho = false
	if !suppress {
		m.appendEntryLocked(msgText, string(p))
	}
	m.mu.Unlock()
	m.notify()
	return len(p), nil
}

// appendEntry appends one structured message and schedules a repaint.
func (m *Model) appendEntry(kind messageKind, text string) {
	m.mu.Lock()
	m.appendEntryLocked(kind, text)
	m.mu.Unlock()
	m.notify()
}

// appendEntryLocked appends one structured message and refreshes the viewport
// content at the current pane width. Callers hold m.mu, so the Agent's
// streamed output and the program loop never race on the pane.
func (m *Model) appendEntryLocked(kind messageKind, text string) {
	m.transcript = append(m.transcript, transcriptEntry{kind: kind, text: text})
	m.viewport.SetContent(renderTranscript(m.transcript, m.paneWidthLocked()))
}

// Indicator implements activity.Sink: the status bar's Working Indicator.
func (m *Model) Indicator(i activity.Indicator) {
	m.mu.Lock()
	m.indicator = i
	m.mu.Unlock()
	m.notify()
}

// ActiveTool implements activity.Sink: the Tool the status bar names while
// the Agent works.
func (m *Model) ActiveTool(name string) {
	m.mu.Lock()
	m.activeTool = name
	m.mu.Unlock()
	m.notify()
}

// Change implements activity.Sink. The shell keeps the Change set the seam
// delivers; the Turn Diff pane (#65) renders it.
func (m *Model) Change(c activity.Change) {
	m.mu.Lock()
	m.changes = append(m.changes, c)
	m.mu.Unlock()
	m.notify()
}

// ToolOutput implements activity.Sink: the pane renders the echoed Tool
// Result — or refusal echo — as a separated, styled block, and drops the
// inline echo the Agent writes for the inline renderer right afterwards.
func (m *Model) ToolOutput(t activity.ToolOutput) {
	m.mu.Lock()
	m.suppressEcho = true
	m.appendEntryLocked(msgToolOutput, formatToolOutputBlock(t))
	m.mu.Unlock()
	m.notify()
}

// CommandReport implements activity.Sink: the pane renders the gated call's
// Command Report as a separated, styled block, and drops the inline echo
// that follows it.
func (m *Model) CommandReport(r activity.CommandReport) {
	m.mu.Lock()
	m.suppressEcho = true
	m.appendEntryLocked(msgCommandReport, formatCommandReportBlock(r))
	m.mu.Unlock()
	m.notify()
}

// Changes returns the Change events delivered so far, in stream order.
func (m *Model) Changes() []activity.Change {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]activity.Change(nil), m.changes...)
}

// ScrollPercent reports how far the Transcript is scrolled (0 at the top, 1
// pinned to the newest content).
func (m *Model) ScrollPercent() float64 {
	m.mu.Lock()
	vp := m.viewport
	m.mu.Unlock()
	vp.Height = m.paneHeight()
	return vp.ScrollPercent()
}

// notify asks the program loop to repaint after the Agent streamed new
// events. The trigger is coalesced into one pending slot and forwarded by a
// dedicated goroutine: a direct Program.Send from inside Update would
// deadlock (the loop cannot receive while it is running Update), and a
// dropped trigger is harmless because the panes read live state and every
// Update repaints anyway.
func (m *Model) notify() {
	m.mu.Lock()
	running := m.running
	m.mu.Unlock()
	if !running {
		return
	}
	select {
	case m.repaint <- struct{}{}:
	default:
	}
}

// repaintLoop forwards coalesced triggers to the program. It runs for the
// lifetime of the program and parks once the program exits.
func (m *Model) repaintLoop() {
	for range m.repaint {
		m.mu.Lock()
		program := m.program
		m.mu.Unlock()
		program.Send(redrawMsg{})
	}
}

// redrawMsg only schedules a repaint; the panes read live state in View.
type redrawMsg struct{}

// turnResultMsg reports a completed Turn so the shell can release the
// running state and surface Git outcomes.
type turnResultMsg struct {
	result agent.TurnResult
	err    error
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	m.mu.Lock()
	m.running = true
	m.mu.Unlock()
	go m.repaintLoop()
	return nil
}

// Update implements tea.Model: the input line, the Transcript scroll
// bindings (mouse wheel, PageUp/PageDown; the ratified Ctrl+E/Ctrl+Y
// bindings arrive with the modeless-input ticket #64), and Turn lifecycle.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case redrawMsg:
		// Nothing to store; View renders live state.
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.mu.Lock()
		m.viewport.Width = msg.Width
		m.viewport.Height = m.paneHeight()
		m.mu.Unlock()
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			return m, tea.Quit
		case tea.KeyEnter:
			return m, m.submit()
		case tea.KeyPgUp:
			m.scrollUp(m.paneHeight())
		case tea.KeyPgDown:
			m.scrollDown(m.paneHeight())
		case tea.KeyBackspace:
			m.input.backspace()
		case tea.KeyLeft:
			m.input.left()
		case tea.KeyRight:
			m.input.right()
		case tea.KeyRunes:
			for _, r := range msg.Runes {
				m.input.insert(r)
			}
		case tea.KeySpace:
			// tea parses a standalone space as KeySpace, not KeyRunes.
			m.input.insert(' ')
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollUp(wheelScrollLines)
		case tea.MouseButtonWheelDown:
			m.scrollDown(wheelScrollLines)
		}
	case turnResultMsg:
		m.turnActive = false
		if msg.err != nil {
			m.Append("deku: " + msg.err.Error() + "\n")
		} else if report := formatResult(msg.result); report != "" {
			m.Append(report)
		}
	}
	return m, nil
}

// submit handles Enter. A pending Approval decision is routed to the
// Approval gate as a line; an idle shell starts a Turn (a "/" line goes to
// the command handler instead); a running Turn is left alone — queuing
// arrives with the modeless-input ticket (#64), not here.
func (m *Model) submit() tea.Cmd {
	m.mu.Lock()
	awaitingApproval := m.indicator == activity.AwaitingApproval
	m.mu.Unlock()
	if awaitingApproval && m.turnActive {
		decision := m.input.take()
		if m.approval != nil {
			if _, err := io.WriteString(m.approval, decision+"\n"); err != nil {
				m.Append(fmt.Sprintf("deku: deliver approval decision: %v\n", err))
			}
		}
		return nil
	}
	if m.turnActive {
		return nil
	}
	request := m.input.take()
	if request == "" {
		return nil
	}
	if strings.HasPrefix(request, "/") {
		m.dispatchCommand(request)
		return nil
	}
	if m.runner == nil {
		m.Append("deku: no Turn runner is attached\n")
		return nil
	}
	m.appendEntry(msgUser, request)
	m.turnActive = true
	return func() tea.Msg {
		result, err := m.runner.Turn(context.Background(), request)
		return turnResultMsg{result: result, err: err}
	}
}

// dispatchCommand runs a "/" line through the command handler and renders
// its reply; an error is reported like any Turn failure.
func (m *Model) dispatchCommand(line string) {
	if m.commands == nil {
		m.Append(fmt.Sprintf("deku: unknown command %q\n", line))
		return
	}
	reply, provider, model, err := m.commands(line)
	if err != nil {
		m.Append("deku: " + err.Error() + "\n")
		return
	}
	if provider != "" && model != "" {
		m.provider, m.model = provider, model
	}
	if reply != "" {
		if !strings.HasSuffix(reply, "\n") {
			reply += "\n"
		}
		m.Append(reply)
	}
}

// scrollUp moves the Transcript view toward the top and leaves the pinned
// follow position; scrollDown moves toward the newest content and re-pins
// once the bottom is reached.
func (m *Model) scrollUp(lines int) {
	m.mu.Lock()
	m.follow = false
	m.viewport.ScrollUp(lines)
	m.mu.Unlock()
}

func (m *Model) scrollDown(lines int) {
	m.mu.Lock()
	m.viewport.ScrollDown(lines)
	if m.viewport.AtBottom() {
		m.follow = true
	}
	m.mu.Unlock()
}

// View implements tea.Model: the Transcript pane, the status bar, and the
// input line.
func (m *Model) View() string {
	width, _ := m.size()
	m.mu.Lock()
	vp := m.viewport
	m.mu.Unlock()
	vp.Width = width
	vp.Height = m.paneHeight()
	if m.follow {
		vp.GotoBottom()
		m.mu.Lock()
		m.viewport.YOffset = vp.YOffset
		m.mu.Unlock()
	}
	return strings.Join([]string{
		vp.View(),
		m.statusBar(),
		m.inputLine(),
	}, "\n")
}

// paneHeight is the Transcript pane's height: the window minus the status
// bar and the input line.
func (m *Model) paneHeight() int {
	_, height := m.size()
	return max(1, height-2)
}

// paneWidthLocked is the Transcript pane's width, defaulting to 80 before the
// first WindowSizeMsg (also how tests render). Callers hold m.mu, which
// guards the width read against the program loop's WindowSizeMsg writes.
func (m *Model) paneWidthLocked() int {
	width, _ := m.size()
	return width
}

// size returns the terminal dimensions, defaulting to 80x24 before the first
// WindowSizeMsg (also how tests render).
func (m *Model) size() (int, int) {
	width, height := m.width, m.height
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}
	return width, height
}

// formatResult renders the Git outcomes of a completed Turn with the same
// wording as the inline renderer's report, so the TUI and the fallback show
// the same facts: stashes, Validation outcomes, and Agent Commits. It is a
// renderer concern; the inline copy lives in cmd/deku.
func formatResult(result agent.TurnResult) string {
	var b strings.Builder
	if result.StashRef != "" {
		fmt.Fprintf(&b, "deku: stashed pre-existing work at %s\n", result.StashRef)
	}
	if result.Validation != nil {
		if result.Validation.Passed {
			fmt.Fprintf(&b, "deku: validation passed (%s)\n", result.Validation.Command)
		} else {
			fmt.Fprintf(&b, "deku: validation failed (%s); work remains uncommitted\n", result.Validation.Command)
		}
	}
	if result.CommitID != "" {
		fmt.Fprintf(&b, "deku: agent commit created %s\n", result.CommitID)
	}
	return b.String()
}

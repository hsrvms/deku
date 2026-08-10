package tui

import (
	"context"
	"errors"
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

// view is which surface the main area shows. Panes are views, not focus
// targets: the Palette and the help overlay replace the Transcript view
// while the status bar and the input line never move (design guide §3).
type view int

// Main-area views.
const (
	viewTranscript view = iota
	viewPalette
	viewHelp
)

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

	// Turn Diff block state. diff is the seam the renderer computes the
	// cumulative working-tree diff through; the remaining fields track the
	// current Turn's path set and the block's visibility and Transcript
	// entry.
	diff          diffFunc
	diffOpen      bool
	diffEntryIdx  int // Transcript index of the current Turn's block, -1 before the first Change
	turnDiffPaths map[string]bool
	diffOrder     []string
	diffCache     map[string]string
	diffErr       error
	diffDirty     bool

	repaint chan struct{} // coalesced redraw triggers for the forwarder

	width      int
	height     int
	input      input
	turnActive bool

	// queue holds messages submitted with Enter while a Turn runs; each
	// runs as the next Turn when the current one completes, in order
	// (modeless input: typing always works).
	queue []string

	// turnCancel cancels the running Turn's context (Ctrl+C interrupts).
	turnCancel context.CancelFunc

	// view is the main-area surface: the Transcript, the model Palette, or
	// the keybinding help overlay.
	view view

	// palette is the model Palette: the filterable, grouped Model list with
	// the current Selection marked (Ctrl+P).
	palette paletteList
}

// New constructs the terminal UI model. provider and model name the current
// Selection for the status bar; approval receives the user's decision lines
// for a pending Approval prompt (the Agent reads them from the other end of
// the pipe). The runner and command handler are attached afterwards with
// SetRunner and SetCommands, because the Agent is constructed with the Model
// as its output Writer and activity Sink.
func New(provider, model string, approval io.Writer) *Model {
	return &Model{
		provider:      provider,
		model:         model,
		approval:      approval,
		follow:        true,
		repaint:       make(chan struct{}, 1),
		diff:          gitDiff(),
		diffEntryIdx:  -1,
		turnDiffPaths: make(map[string]bool),
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

// SetPalette installs the Palette's Models in display order — Provider name
// order, each Provider's Models in declared order — as the /model Command
// lists them. It is attached after New like the runner and commands.
func (m *Model) SetPalette(entries []PaletteEntry) {
	m.palette = *newPalette(entries)
}

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
	m.refreshTranscriptLocked()
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

// Change implements activity.Sink: the Change records the cumulative
// per-file working-tree path set the Turn Diff block renders. The first
// Change of a Turn auto-opens the block inside the Agent's response section
// of the Transcript; every Change marks the diff dirty, because the working
// tree changed and the cumulative diff must be recomputed — a second Edit to
// the same file extends the first (CONTEXT.md: Turn Diff).
func (m *Model) Change(c activity.Change) {
	m.mu.Lock()
	m.changes = append(m.changes, c)
	if m.turnActive {
		m.diffDirty = true
		if _, seen := m.turnDiffPaths[c.Path]; !seen {
			m.turnDiffPaths[c.Path] = true
			m.diffOrder = append(m.diffOrder, c.Path)
			if m.diffEntryIdx < 0 {
				m.diffEntryIdx = len(m.transcript)
				m.diffOpen = true
				m.transcript = append(m.transcript, transcriptEntry{kind: msgTurnDiff})
				m.refreshTranscriptLocked()
			}
		}
	}
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
		m.refreshTranscriptLocked()
		m.mu.Unlock()
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollUp(wheelScrollLines)
		case tea.MouseButtonWheelDown:
			m.scrollDown(wheelScrollLines)
		}
	case turnResultMsg:
		return m, m.finishTurn(msg)
	}
	return m, nil
}

// handleKey dispatches one key: the global chords first — they work from
// every view and input mode — then the transcript-view input keys. The input
// line stays the only focused surface: typing always edits the line.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.interruptOrClear()
	case tea.KeyCtrlD:
		return tea.Quit
	case tea.KeyCtrlP:
		m.openPalette()
	case tea.KeyCtrlE:
		m.scrollDown(1)
	case tea.KeyCtrlY:
		m.scrollUp(1)
	case tea.KeyCtrlT:
		m.toggleDiff()
	}
	switch m.view {
	case viewPalette:
		return m.paletteKey(msg)
	case viewHelp:
		if m.helpKey(msg) {
			return nil
		}
	}
	return m.inputKey(msg)
}

// interruptOrClear implements the ratified Ctrl+C: interrupt the running
// Turn by canceling its context — the Agent exits with a cancellation error
// the shell reports as an interruption — or clear the input line when idle.
func (m *Model) interruptOrClear() {
	m.mu.Lock()
	running := m.turnActive
	cancel := m.turnCancel
	m.mu.Unlock()
	if running && cancel != nil {
		cancel()
		return
	}
	m.input.clear()
}

// inputKey handles the transcript-view keys: vim normal/insert editing on
// the single-line input, command history, and Transcript scrolling.
func (m *Model) inputKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEnter:
		return m.submit()
	case tea.KeyPgUp:
		m.scrollUp(m.paneHeight())
	case tea.KeyPgDown:
		m.scrollDown(m.paneHeight())
	case tea.KeyEsc:
		m.input.normalMode()
	case tea.KeyBackspace:
		if m.input.mode == inputNormal {
			m.input.left()
		} else {
			m.input.backspace()
		}
	case tea.KeyLeft:
		m.input.left()
	case tea.KeyRight:
		m.input.right()
	case tea.KeyUp:
		m.input.historyOlder()
	case tea.KeyDown:
		m.input.historyNewer()
	case tea.KeyRunes:
		if m.input.mode == inputNormal {
			m.normalKey(msg.Runes)
		} else {
			for _, r := range msg.Runes {
				m.input.insert(r)
			}
		}
	case tea.KeySpace:
		// tea parses a standalone space as KeySpace, not KeyRunes.
		if m.input.mode == inputInsert {
			m.input.insert(' ')
		}
	}
	return nil
}

// normalKey dispatches one normal-mode key: the vim editing bindings from
// the ratified keybinding table. Any other character is ignored, so typing
// in normal mode never edits the line.
func (m *Model) normalKey(runes []rune) {
	if len(runes) != 1 {
		return
	}
	// Any key other than d cancels an armed dd, like an operator waiting
	// for its motion in vim.
	if runes[0] != 'd' {
		m.input.cancelDD()
	}
	switch runes[0] {
	case 'i':
		m.input.insertMode()
	case 'a':
		m.input.appendMode()
	case 'A':
		m.input.appendEndMode()
	case 'I':
		m.input.insertStartMode()
	case 'h':
		m.input.left()
	case 'l':
		m.input.right()
	case '0':
		m.input.home()
	case '$':
		m.input.end()
	case 'w':
		m.input.nextWord()
	case 'b':
		m.input.prevWord()
	case 'x':
		m.input.deleteChar()
	case 'd':
		m.input.deleteLineKey()
	case 'j':
		m.input.historyNewer()
	case 'k':
		m.input.historyOlder()
	case '?':
		m.openHelp()
	}
}

// openHelp opens the keybinding help overlay (? in normal mode). The help
// binding is a normal-mode key: insert mode must never trap a typeable
// character, so ? types in insert mode and opens the overlay in normal mode.
func (m *Model) openHelp() { m.view = viewHelp }

// openPalette opens the model Palette view (Ctrl+P): a fresh filter and the
// cursor on the first Model. The Palette is a view over the main area; the
// status bar and the input line stay in place.
func (m *Model) openPalette() {
	m.palette.filter = m.palette.filter[:0]
	m.palette.cursor = 0
	m.view = viewPalette
}

// paletteKey handles keys while the Palette view is open: Esc closes it,
// Enter applies the highlighted Model's Selection override through the same
// command path as /model, ↑/↓ move, and printable characters filter. The
// filter uses the arrow keys for movement because j/k are filter characters.
func (m *Model) paletteKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.view = viewTranscript
	case tea.KeyEnter:
		if entry, ok := m.palette.choice(); ok {
			m.view = viewTranscript
			return m.applyPaletteChoice(entry)
		}
	case tea.KeyUp:
		m.palette.move(-1)
	case tea.KeyDown:
		m.palette.move(1)
	case tea.KeyBackspace:
		m.palette.backspace()
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			m.palette.typeRune(r)
		}
	case tea.KeySpace:
		// tea parses a standalone space as KeySpace, not KeyRunes.
		m.palette.typeRune(' ')
	}
	return nil
}

// applyPaletteChoice runs the /model Command for the chosen entry as a tea
// command, so the per-Session Selection override is set exactly like the
// typed command — same resolution, same Session recording, same reply —
// while the program loop never blocks on the Agent's Selection lock during a
// running Turn.
func (m *Model) applyPaletteChoice(entry PaletteEntry) tea.Cmd {
	return func() tea.Msg {
		m.dispatchCommand("/model " + entry.Provider + " " + entry.Model)
		return nil
	}
}

// helpKey handles keys while the help overlay is open: Esc, Enter, or ?
// close it; everything else keeps editing the input line underneath, so the
// modeless input stays active.
func (m *Model) helpKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyEnter:
		m.view = viewTranscript
		return true
	case tea.KeyRunes:
		if len(msg.Runes) == 1 && msg.Runes[0] == '?' {
			m.view = viewTranscript
			return true
		}
	}
	return false
}

// submit handles Enter. A pending Approval decision is routed to the
// Approval gate as a line; an idle shell starts a Turn (a "/" line goes to
// the command handler instead); a running Turn queues the message as the
// next Turn, so Enter always accepts the line while the Agent works.
func (m *Model) submit() tea.Cmd {
	m.mu.Lock()
	awaitingApproval := m.indicator == activity.AwaitingApproval
	running := m.turnActive
	m.mu.Unlock()
	if awaitingApproval && running {
		decision := m.input.take()
		if decision == "" {
			return nil
		}
		if m.approval != nil {
			if _, err := io.WriteString(m.approval, decision+"\n"); err != nil {
				m.Append(fmt.Sprintf("deku: deliver approval decision: %v\n", err))
			}
		}
		return nil
	}
	request := m.input.take()
	if request == "" {
		return nil
	}
	m.input.remember(request)
	if strings.HasPrefix(request, "/") {
		m.dispatchCommand(request)
		return nil
	}
	if m.runner == nil {
		m.Append("deku: no Turn runner is attached\n")
		return nil
	}
	m.appendEntry(msgUser, request)
	if running {
		m.mu.Lock()
		m.queue = append(m.queue, request)
		m.mu.Unlock()
		return nil
	}
	return m.startTurn(request)
}

// startTurn starts a Turn for request: a fresh Turn Diff (the completed
// Turn's block stays in the Transcript as history; the new one re-opens on
// its first Change), a cancelable context (Ctrl+C interrupts this Turn), and
// the runner command whose result the shell handles when the Turn completes.
func (m *Model) startTurn(request string) tea.Cmd {
	m.mu.Lock()
	m.turnDiffPaths = make(map[string]bool)
	m.diffOrder = nil
	m.diffCache = nil
	m.diffErr = nil
	m.diffDirty = true
	m.diffOpen = false
	m.diffEntryIdx = -1
	m.turnActive = true
	ctx, cancel := context.WithCancel(context.Background())
	m.turnCancel = cancel
	m.mu.Unlock()
	return func() tea.Msg {
		result, err := m.runner.Turn(ctx, request)
		return turnResultMsg{result: result, err: err}
	}
}

// finishTurn releases a completed Turn — reporting its Git outcomes, or the
// interruption when its context was canceled — and starts the next queued
// Turn, so messages queued with Enter while a Turn runs execute in order.
func (m *Model) finishTurn(msg turnResultMsg) tea.Cmd {
	m.mu.Lock()
	m.turnActive = false
	m.turnCancel = nil
	m.mu.Unlock()
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			m.Append("deku: turn interrupted\n")
		} else {
			m.Append("deku: " + msg.err.Error() + "\n")
		}
	} else if report := formatResult(msg.result); report != "" {
		m.Append(report)
	}
	m.mu.Lock()
	var next string
	if len(m.queue) > 0 {
		next = m.queue[0]
		m.queue = m.queue[1:]
	}
	m.mu.Unlock()
	if next == "" {
		return nil
	}
	return m.startTurn(next)
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
		m.mu.Lock()
		m.provider, m.model = provider, model
		m.mu.Unlock()
	}
	if reply != "" {
		if !strings.HasSuffix(reply, "\n") {
			reply += "\n"
		}
		m.Append(reply)
	}
}

// toggleDiff hides and shows the current Turn's Turn Diff block. A completed
// Turn's block stays in the Transcript as history either way.
func (m *Model) toggleDiff() {
	m.mu.Lock()
	m.diffOpen = !m.diffOpen
	m.refreshTranscriptLocked()
	m.mu.Unlock()
}

// refreshDiff recomputes the Turn Diff when the path set changed since the
// last render and writes the block's content. The diff runs outside the lock
// so the Agent's Sink calls never wait on git; a Change that arrives
// mid-compute marks the block dirty again and the next View recomputes.
func (m *Model) refreshDiff() {
	m.mu.Lock()
	if !m.diffOpen || !m.diffDirty {
		m.mu.Unlock()
		return
	}
	paths := append([]string(nil), m.diffOrder...)
	runner := m.diff
	m.diffDirty = false
	m.mu.Unlock()

	diffs, err := runner(paths)
	m.mu.Lock()
	m.diffCache = diffs
	m.diffErr = err
	if m.diffEntryIdx >= 0 && m.diffEntryIdx < len(m.transcript) {
		m.transcript[m.diffEntryIdx].text = formatTurnDiff(diffs, m.diffOrder, err)
	}
	m.refreshTranscriptLocked()
	m.mu.Unlock()
}

// refreshTranscriptLocked re-lays the Transcript at the current pane size,
// skipping the current Turn's Turn Diff block while it is toggled off.
// Callers hold m.mu.
func (m *Model) refreshTranscriptLocked() {
	entries := m.transcript
	if m.diffEntryIdx >= 0 && !m.diffOpen {
		entries = append(append([]transcriptEntry(nil), m.transcript[:m.diffEntryIdx]...), m.transcript[m.diffEntryIdx+1:]...)
	}
	width, _ := m.size()
	m.viewport.Width = width
	m.viewport.Height = m.paneHeight()
	m.viewport.SetContent(renderTranscript(entries, width))
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

// View implements tea.Model: the Transcript pane — which hosts the Turn Diff
// block inside the Agent's response section — the status bar, and the input
// line. The layout never changes when the block appears; the Palette and the
// help overlay replace the Transcript view in place.
func (m *Model) View() string {
	width, _ := m.size()
	m.refreshDiff()
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
	main := vp.View()
	switch m.view {
	case viewPalette:
		main = m.palette.render(m.paneHeight(), m.provider, m.model)
	case viewHelp:
		main = renderHelp(m.paneHeight())
	}
	return strings.Join([]string{main, m.statusBar(), m.inputLine()}, "\n")
}

// paneHeight is the Transcript pane's height: the window minus the status
// bar and the input line.
func (m *Model) paneHeight() int {
	_, height := m.size()
	return max(1, height-2)
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

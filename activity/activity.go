// Package activity defines the Agent-to-display activity stream. The Agent
// emits Working Indicator transitions, typed display events, and change
// events so any renderer — the v1 terminal UI in particular — can show a
// Working Indicator, Tool Output and Command Report blocks, and a live Turn
// Diff from one authoritative source. The CLI and all renderers are consumers
// of this seam; this package defines the stream, not a renderer.
package activity

// Indicator is the Agent's reported turn state for a Working Indicator.
type Indicator string

// Working Indicator transitions the Agent emits.
const (
	// Idle means no Turn is in progress. The Agent reports it whenever a
	// Turn completes — on success, failure, or interruption — so a renderer
	// never claims thinking between Turns.
	Idle Indicator = "idle"
	// Thinking means the Model has been called and the Agent waits for its
	// first output. A silent Model call is work, not a hang.
	Thinking Indicator = "thinking"
	// Working means a Tool is currently executing.
	Working Indicator = "working"
	// AwaitingApproval means the Agent loop is paused waiting for a user
	// decision on a gated Tool Call.
	AwaitingApproval Indicator = "awaiting-approval"
)

// Change is a mutation event for a single Edit or Write, surfaced so a renderer
// can build a live Turn Diff. Tool distinguishes edit from write; Path is the
// repository-relative file the Agent changed.
type Change struct {
	Tool string
	Path string
}

// ToolOutput is a typed display event for a Tool's echoed result — or its
// refusal echo — carrying the facts the Agent renders, so a display can show
// the block its own way instead of parsing the inline renderer's text. Tier
// is the effective Approval tier, omitted (empty) when the Tool is unknown,
// as for a refused call to an undeclared Tool (CONTEXT.md: Tool Output).
type ToolOutput struct {
	Name    string
	Tier    string
	Content string
}

// CommandReport is a typed display event for a gated Tool Call's Command
// Report: the concrete action the call would take, forwarded so a display can
// show it as a distinct block at the point of Approval. Tier is the effective
// tier under the Approval policy, matching what the Approval gate displays.
type CommandReport struct {
	ToolName string
	Tier     string
	Report   string
}

// Sink receives the Agent's activity stream. The Agent calls Indicator,
// ActiveTool, Change, ToolOutput, and CommandReport as a Turn progresses;
// implementations must be safe for sequential use and must not block the Agent
// loop.
//
// ActiveTool names the Tool the Agent is about to execute, reported at the
// moment execution begins, so a renderer's status bar can show which Tool is
// active while the Working indicator is showing without deriving Turn state
// itself (ADR-0010).
//
// ToolOutput is emitted at the point the Agent echoes a Tool's result or a
// refusal, and CommandReport when a gated call's Command Report is rendered
// before an Approval decision is sought, so a renderer can show the same
// blocks the inline renderer formats from the same facts — its own way,
// without parsing the inline text.
type Sink interface {
	Indicator(Indicator)
	ActiveTool(name string)
	Change(Change)
	ToolOutput(ToolOutput)
	CommandReport(CommandReport)
}

// discard is a Sink that drops every event.
type discard struct{}

func (discard) Indicator(Indicator)         {}
func (discard) ActiveTool(string)           {}
func (discard) Change(Change)               {}
func (discard) ToolOutput(ToolOutput)       {}
func (discard) CommandReport(CommandReport) {}

// Discard returns a Sink that drops all activity. It is the default when no
// sink is configured, so an Agent without a display still runs normally.
func Discard() Sink { return discard{} }

// Package activity defines the Agent-to-display activity stream. The Agent
// emits Working Indicator transitions and change events so any renderer — the
// v1 terminal UI in particular — can show a Working Indicator and a live Turn
// Diff from one authoritative source. The CLI and all renderers are consumers
// of this seam; this package defines the stream, not a renderer.
package activity

// Indicator is the Agent's reported turn state for a Working Indicator.
type Indicator string

// Working Indicator transitions the Agent emits.
const (
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

// Sink receives the Agent's activity stream. The Agent calls Indicator and
// Change as a Turn progresses; implementations must be safe for sequential use
// and must not block the Agent loop.
type Sink interface {
	Indicator(Indicator)
	Change(Change)
}

// discard is a Sink that drops every event.
type discard struct{}

func (discard) Indicator(Indicator) {}
func (discard) Change(Change)       {}

// Discard returns a Sink that drops all activity. It is the default when no
// sink is configured, so an Agent without a display still runs normally.
func Discard() Sink { return discard{} }

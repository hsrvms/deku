package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hsrvms/deku/activity"
)

// messageKind identifies what a Transcript entry is, so the pane can render
// each kind distinctly: streamed model text stays plain, user requests are
// right-aligned, and Tool Output and Command Report blocks are separated
// sections with styled headers.
type messageKind int

// Transcript message kinds.
const (
	msgText          messageKind = iota // streamed model text and other plain text
	msgUser                             // a user request, right-aligned
	msgToolOutput                       // a Tool Output block
	msgCommandReport                    // a Command Report block
)

// transcriptEntry is one structured message in the Transcript pane. text
// holds the message's rendered body; the pane adds separators and alignment
// when it lays the entries out at the current width.
type transcriptEntry struct {
	kind messageKind
	text string
}

// renderTranscript lays the structured messages out as the viewport content:
// a separator at each exchange boundary (before every user request), a
// separated block for each Tool Output and Command Report, and plain text
// everywhere else. width is the pane width the content is laid out at.
func renderTranscript(entries []transcriptEntry, width int) string {
	var b strings.Builder
	for _, e := range entries {
		switch e.kind {
		case msgUser:
			b.WriteString(rule(width))
			b.WriteString(lipgloss.NewStyle().Foreground(palette.user).Width(width).Align(lipgloss.Right).Render(e.text))
			b.WriteByte('\n')
		case msgToolOutput, msgCommandReport:
			b.WriteString(rule(width))
			b.WriteString(e.text)
			b.WriteString(rule(width))
		default:
			b.WriteString(e.text)
		}
	}
	return b.String()
}

// rule is a full-width section separator marking an exchange boundary or
// framing a typed block. It is gray so it never reads as a state.
func rule(width int) string {
	if width <= 0 {
		width = 80
	}
	return lipgloss.NewStyle().Foreground(palette.rule).Render(strings.Repeat("─", width)) + "\n"
}

// formatToolOutputBlock renders a Tool Output block body: a header naming the
// Tool and its effective tier, then the indented content — the same facts the
// inline renderer formats its own way. The tier is omitted when it is
// unknown, as for a refused call to an undeclared Tool.
func formatToolOutputBlock(t activity.ToolOutput) string {
	return blockBody(blockHeader("Tool output", t.Name, t.Tier), palette.toolOutput, t.Content)
}

// formatCommandReportBlock renders a Command Report block body: a header
// naming the Tool and its effective tier, then the indented Report lines.
func formatCommandReportBlock(r activity.CommandReport) string {
	return blockBody(blockHeader("Command Report", r.ToolName, r.Tier), palette.commandReport, r.Report)
}

// blockHeader names a typed block, mirroring the inline renderer's facts:
// "Tool output (read)" or "Command Report (write, read)".
func blockHeader(kind, name, tier string) string {
	header := kind
	if name != "" {
		header += " (" + name
		if tier != "" {
			header += ", " + tier
		}
		header += ")"
	}
	return header + ":"
}

// blockBody renders a styled header followed by the content indented two
// spaces, so a typed block scans like the inline renderer's block while
// staying distinct from streamed model text.
func blockBody(header string, color lipgloss.Color, content string) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(color).Render(header))
	b.WriteByte('\n')
	for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

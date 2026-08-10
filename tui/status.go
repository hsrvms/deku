package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hsrvms/deku/activity"
)

// palette holds the semantic color tokens. Tokens are the only place raw
// colors appear; components render through them, and the Working Indicator
// always pairs a glyph and a label with its color, so no state is conveyed by
// color alone (design guide §6). The baseline is 16-color-safe and chosen for
// contrast on dark terminals.
type paletteTokens struct {
	thinking      lipgloss.Color
	working       lipgloss.Color
	awaiting      lipgloss.Color
	prompt        lipgloss.Color
	user          lipgloss.Color
	toolOutput    lipgloss.Color
	commandReport lipgloss.Color
	rule          lipgloss.Color
	idle          lipgloss.Color
	diffAdd       lipgloss.Color
	diffDel       lipgloss.Color
}

var palette = paletteTokens{
	thinking:      lipgloss.Color("11"), // bright yellow
	working:       lipgloss.Color("14"), // bright cyan
	awaiting:      lipgloss.Color("9"),  // bright red
	prompt:        lipgloss.Color("10"), // bright green
	user:          lipgloss.Color("12"), // bright blue
	toolOutput:    lipgloss.Color("6"),  // cyan
	commandReport: lipgloss.Color("13"), // bright magenta
	rule:          lipgloss.Color("8"),  // gray
	idle:          lipgloss.Color("8"),  // gray
	diffAdd:       lipgloss.Color("10"), // bright green
	diffDel:       lipgloss.Color("9"),  // bright red
}

// indicatorStyle maps a Working Indicator state to its glyph, label, and
// semantic color. The triple redundancy is deliberate: the state is never
// conveyed by color alone. The zero indicator (no event yet) renders nothing.
func indicatorStyle(i activity.Indicator) (glyph, label string, color lipgloss.Color) {
	switch i {
	case activity.Idle:
		return "●", "idle", palette.idle
	case activity.Thinking:
		return "●", "thinking", palette.thinking
	case activity.Working:
		return "▶", "working", palette.working
	case activity.AwaitingApproval:
		return "?", "awaiting approval", palette.awaiting
	default:
		return "", "", ""
	}
}

// statusBar renders the Working Indicator (glyph, label, and color), the
// active Tool, and the current Provider/Model. Idle (no indicator event yet)
// shows just the Tool and the Selection, so the bar is always visible and
// never claims a state the Agent did not report; a completed Turn's Idle
// state is reported by the Agent, never inferred by the shell.
func (m *Model) statusBar() string {
	m.mu.Lock()
	indicator, activeTool, provider, model := m.indicator, m.activeTool, m.provider, m.model
	m.mu.Unlock()
	parts := make([]string, 0, 3)
	if indicator != "" {
		glyph, label, color := indicatorStyle(indicator)
		parts = append(parts, lipgloss.NewStyle().Foreground(color).Render(glyph+" "+label))
	}
	if activeTool != "" {
		parts = append(parts, "tool: "+activeTool)
	}
	parts = append(parts, provider+"/"+model)
	return strings.Join(parts, " · ")
}

// inputLine renders the single-line input with its prompt and cursor. While
// the Palette is open the line is the Palette's filter; while the Agent
// awaits an Approval decision it is the Command Report prompt with the
// available decisions, so Approval renders in the input area with the status
// bar showing awaiting Approval (design guide §3). In normal mode the prompt
// carries a [normal] tag, so the mode is never conveyed by color alone.
func (m *Model) inputLine() string {
	if m.view == viewPalette {
		prompt := lipgloss.NewStyle().Foreground(palette.prompt).Render("filter: ")
		return prompt + string(m.palette.filter) + "█"
	}
	m.mu.Lock()
	awaiting := m.indicator == activity.AwaitingApproval
	m.mu.Unlock()
	prompt, color := "> ", palette.prompt
	if awaiting {
		prompt, color = "Approve? [y/n] ", palette.awaiting
	}
	if m.input.mode == inputNormal {
		prompt = "[normal] " + prompt
	}
	return lipgloss.NewStyle().Foreground(color).Render(prompt) + m.input.render()
}

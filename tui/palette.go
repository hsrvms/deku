package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PaletteEntry names one selectable Model in the Palette. Entries are in
// display order: Provider name order, each Provider's Models in declared
// order, as the /model Command lists them.
type PaletteEntry struct {
	Provider string
	Model    string
}

// paletteList is the model Palette: an interactive, filterable list of Models
// grouped by Provider with the current Selection marked. Ctrl+P opens it as
// the main-area view and the input line becomes the filter line; choosing an
// entry applies the per-Session Selection override through the same command
// path as /model. It is only ever touched by the program loop, so it needs
// no locking.
type paletteList struct {
	entries []PaletteEntry
	filter  []rune
	cursor  int // index into the filtered list
}

func newPalette(entries []PaletteEntry) *paletteList {
	return &paletteList{entries: entries}
}

// filtered returns the entries matching the filter: a case-insensitive
// substring match on the Provider name or the Model name.
func (p *paletteList) filtered() []PaletteEntry {
	if len(p.filter) == 0 {
		return p.entries
	}
	needle := strings.ToLower(string(p.filter))
	var out []PaletteEntry
	for _, e := range p.entries {
		if strings.Contains(strings.ToLower(e.Provider), needle) ||
			strings.Contains(strings.ToLower(e.Model), needle) {
			out = append(out, e)
		}
	}
	return out
}

// typeRune appends one filter rune and resets the cursor to the first match.
func (p *paletteList) typeRune(r rune) {
	p.filter = append(p.filter, r)
	p.cursor = 0
}

// backspace removes the last filter rune and resets the cursor.
func (p *paletteList) backspace() {
	if len(p.filter) > 0 {
		p.filter = p.filter[:len(p.filter)-1]
		p.cursor = 0
	}
}

// move shifts the cursor by delta, clamped to the filtered list.
func (p *paletteList) move(delta int) {
	if n := len(p.filtered()); n > 0 {
		p.cursor += delta
		if p.cursor < 0 {
			p.cursor = 0
		}
		if p.cursor >= n {
			p.cursor = n - 1
		}
	}
}

// choice returns the highlighted entry; ok is false when nothing matches.
func (p *paletteList) choice() (entry PaletteEntry, ok bool) {
	filtered := p.filtered()
	if p.cursor < 0 || p.cursor >= len(filtered) {
		return PaletteEntry{}, false
	}
	return filtered[p.cursor], true
}

// render draws the Palette: a title line, the Providers grouped with their
// Models, the cursor marker (▶) on the highlighted Model, and the current
// Selection marker (●) on the selected Model. The list is windowed around
// the cursor so the status bar and the input line never move.
func (p *paletteList) render(height int, provider, model string) string {
	selected := provider + "/" + model
	title := lipgloss.NewStyle().Foreground(palette.prompt).Render(
		"Models — type to filter, ↑/↓ move, Enter choose, Esc close")
	lines := []string{title}
	filtered := p.filtered()
	if len(filtered) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(palette.rule).Render(p.emptyMessage()))
		return windowLines(lines, height, 0)
	}
	cursorLine := 0
	currentGroup := ""
	for i, e := range filtered {
		if e.Provider != currentGroup {
			currentGroup = e.Provider
			lines = append(lines, lipgloss.NewStyle().Foreground(palette.rule).Render(e.Provider))
		}
		style := lipgloss.NewStyle()
		cursorMark := " "
		if i == p.cursor {
			cursorMark = "▶"
			cursorLine = len(lines)
			style = style.Foreground(palette.prompt)
		}
		selectionMark := " "
		if e.Provider+"/"+e.Model == selected {
			selectionMark = "●"
		}
		lines = append(lines, style.Render("  "+cursorMark+" "+selectionMark+" "+e.Model))
	}
	return windowLines(lines, height, cursorLine)
}

// emptyMessage names why nothing is listed, mirroring the /model Command's
// wording so the Palette and the Command never diverge.
func (p *paletteList) emptyMessage() string {
	if len(p.entries) == 0 {
		return "no providers can authenticate; declare providers in models.json and credentials in auth.json"
	}
	return "no models match the filter"
}

// windowLines clips lines around cursorLine to at most height lines, so an
// entry list longer than the pane never moves the status bar or the input
// line.
func windowLines(lines []string, height, cursorLine int) string {
	if len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	start := cursorLine - height/2
	if start < 0 {
		start = 0
	}
	if start+height > len(lines) {
		start = len(lines) - height
	}
	return strings.Join(lines[start:start+height], "\n")
}

// renderHelp draws the keybinding help overlay: every binding from the
// ratified keybinding table plus the shell's scroll and quit keys, so the
// small binding set is discoverable (? from normal mode).
func renderHelp(height int) string {
	lines := []string{
		lipgloss.NewStyle().Foreground(palette.prompt).Render("Keybindings"),
		"  Enter            submit; queue as the next Turn while one runs",
		"  Ctrl+C           interrupt the running Turn; clear the input when idle",
		"  Ctrl+P           open the model Palette",
		"  Ctrl+E / Ctrl+Y  scroll the Transcript down / up",
		"  Ctrl+T           toggle the current Turn Diff block",
		"  Ctrl+D           quit",
		"  Esc              leave insert mode; i / a / A / I return to it",
		"  h l 0 $ w b      move: left / right / start / end / word back / word forward",
		"  x / dd           delete the character / the whole line",
		"  j / k            command history: newer / older (normal mode)",
		"  ↑ / ↓            command history (any mode)",
		"  PgUp / PgDn      scroll the Transcript",
		"  ?                show this help (normal mode)",
	}
	return windowLines(lines, height, 0)
}

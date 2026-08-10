package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// testPaletteEntries is the fixture list for Palette tests: two Providers,
// one with two Models, in the order /model lists them.
var testPaletteEntries = []PaletteEntry{
	{Provider: "openai", Model: "gpt-4o"},
	{Provider: "openai", Model: "gpt-4o-mini"},
	{Provider: "tokenrouter", Model: "qwen-2.5-coder"},
}

func ctrlP() tea.Msg { return tea.KeyMsg{Type: tea.KeyCtrlP} }

func TestPaletteOpensGroupedWithSelectionMarked(t *testing.T) {
	m := newTestModel(io.Discard)
	m.SetPalette(testPaletteEntries)

	m.Update(ctrlP())
	if m.view != viewPalette {
		t.Fatal("Ctrl+P must open the Palette")
	}
	view := stripANSI(m.View())
	for _, want := range []string{"Models", "openai", "gpt-4o", "gpt-4o-mini", "tokenrouter", "qwen-2.5-coder"} {
		if !strings.Contains(view, want) {
			t.Errorf("Palette must show %q, got %q", want, view)
		}
	}
	// The current Selection (tokenrouter/qwen-2.5-coder) is marked with ●;
	// the highlighted first entry carries the ▶ cursor marker.
	if !strings.Contains(view, "● qwen-2.5-coder") {
		t.Errorf("Palette must mark the current Selection, got %q", view)
	}
	if !strings.Contains(view, "▶   gpt-4o") {
		t.Errorf("Palette must mark the highlighted entry, got %q", view)
	}
	// The input line becomes the filter line; the status bar stays put.
	if got := inputLineText(m); got != "filter: █" {
		t.Errorf("input line must show the filter, got %q", got)
	}
	if !strings.Contains(view, "tokenrouter/qwen-2.5-coder") {
		t.Errorf("status bar must stay visible under the Palette, got %q", view)
	}
}

func TestPaletteFiltersAndClosesWithoutChoosing(t *testing.T) {
	m := newTestModel(io.Discard)
	m.SetPalette(testPaletteEntries)
	m.Update(ctrlP())

	typeText(m, "gpt")
	view := stripANSI(m.View())
	if !strings.Contains(view, "gpt-4o") || strings.Contains(view, "● qwen-2.5-coder") {
		t.Errorf("filtering must narrow the list to matching Models, got %q", view)
	}
	if got := inputLineText(m); got != "filter: gpt█" {
		t.Errorf("the filter line must show the typed filter, got %q", got)
	}
	// A filter with no matches keeps the Palette open and says so.
	typeText(m, "zzz")
	view = stripANSI(m.View())
	if !strings.Contains(view, "no models match the filter") {
		t.Errorf("an unmatched filter must say so, got %q", view)
	}
	if _, cmd := m.Update(enterKey()); cmd != nil {
		t.Fatal("Enter with no match must not choose anything")
	}
	if m.view != viewPalette {
		t.Error("Enter with no match must keep the Palette open")
	}
	// Esc closes without choosing.
	m.Update(escKey())
	if m.view != viewTranscript {
		t.Error("Esc must close the Palette")
	}
}

func TestPaletteChoiceAppliesSelectionLikeModelCommand(t *testing.T) {
	m := newTestModel(io.Discard)
	m.SetPalette(testPaletteEntries)
	var got string
	m.SetCommands(func(line string) (string, string, string, error) {
		got = line
		return "selection: openai / gpt-4o\n", "openai", "gpt-4o", nil
	})

	m.Update(ctrlP())
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // highlight gpt-4o-mini
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // wrap around to the last entry
	_, cmd := m.Update(enterKey())
	if cmd == nil {
		t.Fatal("choosing an entry must apply the Selection")
	}
	if m.view != viewTranscript {
		t.Error("choosing must close the Palette")
	}
	m.Update(cmd())
	if got != "/model tokenrouter qwen-2.5-coder" {
		t.Errorf("Palette choice must run the /model Command, got %q", got)
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "selection: openai / gpt-4o") {
		t.Errorf("the /model reply must render in the Transcript, got %q", view)
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "openai/gpt-4o") {
		t.Errorf("the status bar must name the new Selection, got %q", view)
	}
	// Reopening the Palette marks the new Selection.
	m.Update(ctrlP())
	view := stripANSI(m.View())
	if !strings.Contains(view, "● gpt-4o") {
		t.Errorf("the Palette must mark the new Selection, got %q", view)
	}
}

func TestPaletteChoiceDoesNotBlockWhileTurnRuns(t *testing.T) {
	runner := &blockingRunner{started: make(chan struct{}, 1)}
	m := newTestModel(io.Discard)
	m.SetRunner(runner)
	m.SetPalette(testPaletteEntries)
	var got string
	m.SetCommands(func(line string) (string, string, string, error) {
		got = line
		return "selection: openai / gpt-4o\n", "openai", "gpt-4o", nil
	})

	typeText(m, "hello")
	_, turnCmd := m.Update(enterKey())
	done := make(chan tea.Msg, 1)
	go func() { done <- turnCmd() }()
	<-runner.started

	// Choosing a Model while a Turn runs returns a command immediately —
	// the program loop never blocks on the Agent's Selection lock.
	m.Update(ctrlP())
	_, choiceCmd := m.Update(enterKey())
	if choiceCmd == nil {
		t.Fatal("a Palette choice must return a command even while a Turn runs")
	}
	if m.view != viewTranscript {
		t.Error("choosing must close the Palette")
	}

	// Interrupt the running Turn, then the choice applies.
	m.Update(ctrlC())
	msg := <-done
	if _, next := m.Update(msg); next != nil {
		t.Fatal("nothing may be queued in this test")
	}
	// The choice command runs off the program loop; the loop keeps rendering
	// concurrently, so the shared Selection display must stay lock-protected.
	applied := make(chan struct{})
	go func() {
		m.Update(choiceCmd())
		close(applied)
	}()
	for {
		m.View()
		select {
		case <-applied:
			goto done
		default:
		}
	}
done:
	if got != "/model openai gpt-4o" {
		t.Errorf("Palette choice = %q, want the /model line", got)
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "openai/gpt-4o") {
		t.Errorf("the status bar must name the new Selection, got %q", view)
	}
}

func TestPaletteOpensFromHelpOverlay(t *testing.T) {
	m := newTestModel(io.Discard)
	m.SetPalette(testPaletteEntries)
	m.Update(escKey())
	m.Update(keyRune('?'))
	if m.view != viewHelp {
		t.Fatal("help must be open")
	}
	m.Update(ctrlP())
	if m.view != viewPalette {
		t.Error("Ctrl+P must switch from the help overlay to the Palette")
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "gpt-4o") {
		t.Errorf("Palette must render, got %q", view)
	}
}

func TestEmptyPaletteSaysNoProviders(t *testing.T) {
	m := newTestModel(io.Discard)
	m.Update(ctrlP())
	view := stripANSI(m.View())
	if !strings.Contains(view, "no providers can authenticate") {
		t.Errorf("an empty Palette must say no providers can authenticate, got %q", view)
	}
}

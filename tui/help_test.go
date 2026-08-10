package tui

import (
	"fmt"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestQuestionMarkInInsertModeTypes(t *testing.T) {
	m := newTestModel(io.Discard)
	typeText(m, "what?")
	if got := inputLineText(m); !strings.Contains(got, "what?█") {
		t.Errorf("? in insert mode must type the character, got %q", got)
	}
	if m.view != viewTranscript {
		t.Error("? in insert mode must not open the help overlay")
	}
}

func TestQuestionMarkOpensHelpInNormalMode(t *testing.T) {
	m := newTestModel(io.Discard)
	m.Update(escKey())
	m.Update(keyRune('?'))
	if m.view != viewHelp {
		t.Fatal("? in normal mode must open the help overlay")
	}
	view := stripANSI(m.View())
	for _, want := range []string{
		"Keybindings", "Enter", "Ctrl+C", "Ctrl+P", "Ctrl+E", "Ctrl+Y",
		"Ctrl+T", "Ctrl+D", "dd", "history",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("help overlay must list %q, got %q", want, view)
		}
	}
	// The status bar and the input line stay in place under the overlay.
	if !strings.Contains(view, "tokenrouter/qwen-2.5-coder") {
		t.Errorf("status bar must stay visible under the help overlay, got %q", view)
	}
	if got := inputLineText(m); !strings.Contains(got, "> █") {
		t.Errorf("input line must stay visible under the help overlay, got %q", got)
	}

	// Esc, Enter, and ? close it.
	for _, close := range []tea.Msg{escKey(), enterKey(), keyRune('?')} {
		m.Update(close)
		if m.view != viewTranscript {
			t.Errorf("key %v must close the help overlay", close)
		}
		m.Update(escKey())
		m.Update(keyRune('?'))
		if m.view != viewHelp {
			t.Fatal("help must reopen after closing")
		}
	}
}

func TestTypingWorksUnderHelpOverlay(t *testing.T) {
	m := newTestModel(io.Discard)
	m.Update(escKey())
	m.Update(keyRune('?'))
	if m.view != viewHelp {
		t.Fatal("help must be open")
	}
	// The input stays active under the overlay: typing edits the line, and
	// Enter closes the overlay without submitting the draft.
	m.Update(keyRune('i')) // back to insert mode under the overlay
	typeText(m, "draft")
	if got := inputLineText(m); !strings.Contains(got, "draft█") {
		t.Errorf("typing under the help overlay must edit the input, got %q", got)
	}
	m.Update(enterKey())
	if m.view != viewTranscript {
		t.Error("Enter must close the help overlay")
	}
	if got := inputLineText(m); !strings.Contains(got, "draft█") {
		t.Errorf("Enter under the help overlay must keep the draft, got %q", got)
	}
}

func TestCtrlEYScrollTranscriptFromAnyMode(t *testing.T) {
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12}) // pane height 10
	var content strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&content, "line %02d\n", i)
	}
	if _, err := m.Write([]byte(content.String())); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	m.View() // pin the view to the newest content (follow)
	if p := m.ScrollPercent(); p != 1 {
		t.Fatalf("view must start pinned to the bottom, percent = %v", p)
	}

	// Ctrl+Y scrolls up from insert mode while typing.
	typeText(m, "x")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if p := m.ScrollPercent(); p >= 1 {
		t.Errorf("Ctrl+Y from insert mode must scroll up, percent = %v", p)
	}
	// Ctrl+E scrolls back to the bottom from normal mode.
	m.Update(escKey())
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if p := m.ScrollPercent(); p != 1 {
		t.Errorf("Ctrl+E from normal mode must scroll down, percent = %v", p)
	}
	// The chords do not type into the line.
	if got := inputLineText(m); strings.Contains(got, "x█") == false {
		t.Errorf("the input must keep its text, got %q", got)
	}
}

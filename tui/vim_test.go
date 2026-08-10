package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func escKey() tea.Msg { return tea.KeyMsg{Type: tea.KeyEsc} }

func keyRune(r rune) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func upArrow() tea.Msg   { return tea.KeyMsg{Type: tea.KeyUp} }
func downArrow() tea.Msg { return tea.KeyMsg{Type: tea.KeyDown} }

// inputLineText returns the stripped input-line text (the prompt, the mode
// tag, the value, and the cursor), so tests can assert editing state without
// depending on styling.
func inputLineText(m *Model) string {
	lines := strings.Split(stripANSI(m.View()), "\n")
	return lines[len(lines)-1]
}

func TestVimEscEntersNormalMode(t *testing.T) {
	m := newTestModel(io.Discard)
	typeText(m, "hello")
	if got := inputLineText(m); strings.Contains(got, "[normal]") {
		t.Errorf("insert mode must not show a normal-mode tag, got %q", got)
	}
	m.Update(escKey())
	if got := inputLineText(m); !strings.Contains(got, "[normal] > hello█") {
		t.Errorf("Esc must enter normal mode, got %q", got)
	}
	m.Update(keyRune('i'))
	if got := inputLineText(m); strings.Contains(got, "[normal]") {
		t.Errorf("i must return to insert mode, got %q", got)
	}
}

func TestVimNormalModeMovement(t *testing.T) {
	m := newTestModel(io.Discard)
	typeText(m, "hello world")
	m.Update(escKey())

	m.Update(keyRune('h'))
	if got := inputLineText(m); !strings.Contains(got, "worl█d") {
		t.Errorf("h must move the cursor left, got %q", got)
	}
	m.Update(keyRune('l'))
	if got := inputLineText(m); !strings.Contains(got, "world█") {
		t.Errorf("l must move the cursor right, got %q", got)
	}
	m.Update(keyRune('0'))
	if got := inputLineText(m); !strings.Contains(got, "█hello world") {
		t.Errorf("0 must move to the line start, got %q", got)
	}
	m.Update(keyRune('$'))
	if got := inputLineText(m); !strings.Contains(got, "hello world█") {
		t.Errorf("$ must move to the line end, got %q", got)
	}
	m.Update(keyRune('0'))
	m.Update(keyRune('w'))
	if got := inputLineText(m); !strings.Contains(got, "hello █world") {
		t.Errorf("w must move to the next word start, got %q", got)
	}
	m.Update(keyRune('b'))
	if got := inputLineText(m); !strings.Contains(got, "█hello world") {
		t.Errorf("b must move to the previous word start, got %q", got)
	}
	// Movement at the boundaries stays put.
	m.Update(keyRune('h'))
	m.Update(keyRune('b'))
	if got := inputLineText(m); !strings.Contains(got, "█hello world") {
		t.Errorf("h and b at the line start must stay put, got %q", got)
	}
	m.Update(keyRune('$'))
	m.Update(keyRune('l'))
	m.Update(keyRune('w'))
	if got := inputLineText(m); !strings.Contains(got, "hello world█") {
		t.Errorf("l and w at the line end must stay put, got %q", got)
	}
}

func TestVimNormalModeEditingKeys(t *testing.T) {
	m := newTestModel(io.Discard)
	typeText(m, "hello world")
	m.Update(escKey())

	m.Update(keyRune('0'))
	m.Update(keyRune('x'))
	if got := inputLineText(m); !strings.Contains(got, "ello world") {
		t.Errorf("x must delete the character under the cursor, got %q", got)
	}
	m.Update(keyRune('x'))
	if got := inputLineText(m); !strings.Contains(got, "llo world") {
		t.Errorf("x must delete the next character, got %q", got)
	}

	// A lone d arms the line delete; any other key resets it.
	m.Update(keyRune('d'))
	m.Update(keyRune('l'))
	m.Update(keyRune('d'))
	m.Update(keyRune('$'))
	if got := inputLineText(m); !strings.Contains(got, "llo world█") {
		t.Errorf("d after a non-d key must not delete the line, got %q", got)
	}
	m.Update(keyRune('d'))
	m.Update(keyRune('d'))
	if got := inputLineText(m); !strings.Contains(got, "[normal] > █") {
		t.Errorf("dd must delete the whole line, got %q", got)
	}
}

func TestVimInsertModeEntryKeys(t *testing.T) {
	m := newTestModel(io.Discard)
	typeText(m, "ab")
	m.Update(escKey())
	m.Update(keyRune('0'))
	m.Update(keyRune('i'))
	typeText(m, "X")
	if got := inputLineText(m); !strings.Contains(got, "X█ab") {
		t.Errorf("i must insert at the cursor, got %q", got)
	}
	m.Update(escKey())
	m.Update(keyRune('a'))
	typeText(m, "Y")
	if got := inputLineText(m); !strings.Contains(got, "XaY█b") {
		t.Errorf("a must insert after the cursor, got %q", got)
	}
	m.Update(escKey())
	m.Update(keyRune('I'))
	typeText(m, "Z")
	if got := inputLineText(m); !strings.Contains(got, "Z█XaYb") {
		t.Errorf("I must insert at the start, got %q", got)
	}
	m.Update(escKey())
	m.Update(keyRune('A'))
	typeText(m, "W")
	if got := inputLineText(m); !strings.Contains(got, "ZXaYbW█") {
		t.Errorf("A must insert at the end, got %q", got)
	}
}

func TestVimCommandHistoryJK(t *testing.T) {
	runner := &stubRunner{}
	m := newTestModel(io.Discard)
	m.SetRunner(runner)

	typeText(m, "first")
	_, cmd := m.Update(enterKey())
	completeTurn(t, m, cmd)
	typeText(m, "second")
	_, cmd = m.Update(enterKey())
	completeTurn(t, m, cmd)

	m.Update(escKey())
	m.Update(keyRune('k'))
	if got := inputLineText(m); !strings.Contains(got, "second█") {
		t.Errorf("k must recall the newest history entry, got %q", got)
	}
	m.Update(keyRune('k'))
	if got := inputLineText(m); !strings.Contains(got, "first█") {
		t.Errorf("k must walk back through history, got %q", got)
	}
	m.Update(keyRune('k'))
	if got := inputLineText(m); !strings.Contains(got, "first█") {
		t.Errorf("k at the oldest entry must stay put, got %q", got)
	}
	m.Update(keyRune('j'))
	if got := inputLineText(m); !strings.Contains(got, "second█") {
		t.Errorf("j must walk forward through history, got %q", got)
	}
	m.Update(keyRune('j'))
	if got := inputLineText(m); !strings.Contains(got, "> █") {
		t.Errorf("j past the newest entry must return to the fresh line, got %q", got)
	}

	// Editing a recalled line breaks the browse: k starts from the newest.
	m.Update(keyRune('k'))
	m.Update(keyRune('k'))
	m.Update(keyRune('i'))
	typeText(m, "X")
	m.Update(escKey())
	m.Update(keyRune('k'))
	if got := inputLineText(m); !strings.Contains(got, "second█") {
		t.Errorf("editing a recalled line must reset the browse position, got %q", got)
	}

	// The arrow keys browse history from insert mode too.
	m.Update(upArrow())
	if got := inputLineText(m); !strings.Contains(got, "first█") {
		t.Errorf("Up must walk back through history, got %q", got)
	}
	m.Update(downArrow())
	if got := inputLineText(m); !strings.Contains(got, "second█") {
		t.Errorf("Down must walk forward through history, got %q", got)
	}
}

func TestEnterSubmitsFromNormalMode(t *testing.T) {
	runner := &stubRunner{}
	m := newTestModel(io.Discard)
	m.SetRunner(runner)

	typeText(m, "hello")
	m.Update(escKey())
	_, cmd := m.Update(enterKey())
	if cmd == nil {
		t.Fatal("Enter in normal mode must submit the line")
	}
	completeTurn(t, m, cmd)
	if len(runner.requests) != 1 || runner.requests[0] != "hello" {
		t.Errorf("runner requests = %#v, want [hello]", runner.requests)
	}
	// Submitting returns to insert mode.
	if got := inputLineText(m); strings.Contains(got, "[normal]") {
		t.Errorf("submitting must return to insert mode, got %q", got)
	}
}

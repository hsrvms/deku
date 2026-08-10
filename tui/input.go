package tui

// input is the shell's single-line input with vim normal/insert editing and
// command history: typing and cursor motion in insert mode; Esc, i/a/A/I,
// h/l/0/$, w/b, x, dd, and j/k history in normal mode (the ratified
// keybinding table). It is only ever touched by the program loop, so it
// needs no locking.
type input struct {
	runes  []rune
	cursor int
	mode   inputMode

	// history holds every submitted non-empty line, oldest first; historyAt
	// is the entry the line currently shows, or -1 while the line is a fresh
	// edit. k walks back (older), j walks forward (newer).
	history   []string
	historyAt int

	// pendingDD arms the dd line delete: a second d deletes the whole line,
	// any other key resets it.
	pendingDD bool
}

// inputMode is the vim mode of the input line.
type inputMode int

// Vim modes.
const (
	inputInsert inputMode = iota
	inputNormal
)

// insert places r at the cursor and advances. Editing the recalled line
// breaks the history browse, like a shell.
func (in *input) insert(r rune) {
	in.runes = append(in.runes, 0)
	copy(in.runes[in.cursor+1:], in.runes[in.cursor:])
	in.runes[in.cursor] = r
	in.cursor++
	in.historyAt = -1
	in.pendingDD = false
}

// backspace deletes the rune before the cursor.
func (in *input) backspace() {
	if in.cursor == 0 || len(in.runes) == 0 {
		return
	}
	in.runes = append(in.runes[:in.cursor-1], in.runes[in.cursor:]...)
	in.cursor--
	in.historyAt = -1
	in.pendingDD = false
}

func (in *input) left() {
	if in.cursor > 0 {
		in.cursor--
	}
}

func (in *input) right() {
	if in.cursor < len(in.runes) {
		in.cursor++
	}
}

// home moves to the start of the line (0).
func (in *input) home() { in.cursor = 0 }

// end moves to the end of the line ($).
func (in *input) end() { in.cursor = len(in.runes) }

// nextWord moves to the start of the next word (w): a word is a run of
// non-space runes, so the cursor lands on the first rune after the next
// space run.
func (in *input) nextWord() {
	n := len(in.runes)
	i := in.cursor
	for i < n && in.runes[i] != ' ' {
		i++
	}
	for i < n && in.runes[i] == ' ' {
		i++
	}
	in.cursor = i
}

// prevWord moves to the start of the previous word (b).
func (in *input) prevWord() {
	i := in.cursor
	for i > 0 && in.runes[i-1] == ' ' {
		i--
	}
	for i > 0 && in.runes[i-1] != ' ' {
		i--
	}
	in.cursor = i
}

// deleteChar deletes the rune under the cursor (x); the following rune
// slides onto it.
func (in *input) deleteChar() {
	if in.cursor < len(in.runes) {
		in.runes = append(in.runes[:in.cursor], in.runes[in.cursor+1:]...)
	}
	in.historyAt = -1
	in.pendingDD = false
}

// deleteLineKey handles the d key: a second consecutive d deletes the whole
// line (dd); a lone d arms it.
func (in *input) deleteLineKey() {
	if in.pendingDD {
		in.runes = in.runes[:0]
		in.cursor = 0
		in.historyAt = -1
		in.pendingDD = false
		return
	}
	in.pendingDD = true
}

// cancelDD disarms a pending dd; any normal-mode key other than d calls it.
func (in *input) cancelDD() { in.pendingDD = false }

// insertMode enters insert mode at the cursor (i).
func (in *input) insertMode() { in.mode = inputInsert; in.pendingDD = false }

// appendMode enters insert mode after the cursor (a).
func (in *input) appendMode() {
	if in.cursor < len(in.runes) {
		in.cursor++
	}
	in.mode = inputInsert
	in.pendingDD = false
}

// insertStartMode enters insert mode at the start of the line (I).
func (in *input) insertStartMode() { in.cursor = 0; in.insertMode() }

// appendEndMode enters insert mode at the end of the line (A).
func (in *input) appendEndMode() { in.cursor = len(in.runes); in.insertMode() }

// normalMode leaves insert mode (Esc).
func (in *input) normalMode() { in.mode = inputNormal; in.pendingDD = false }

// remember records a submitted line as the newest history entry.
func (in *input) remember(line string) {
	if line == "" {
		return
	}
	in.history = append(in.history, line)
	in.historyAt = -1
}

// historyOlder shows the previous history entry (k, Up); the oldest entry
// keeps the browse position.
func (in *input) historyOlder() {
	if len(in.history) == 0 {
		return
	}
	if in.historyAt < 0 {
		in.historyAt = len(in.history) - 1
	} else if in.historyAt > 0 {
		in.historyAt--
	}
	in.loadHistory()
}

// historyNewer shows the next history entry (j, Down); past the newest it
// returns to the fresh line.
func (in *input) historyNewer() {
	if in.historyAt < 0 {
		return
	}
	if in.historyAt < len(in.history)-1 {
		in.historyAt++
		in.loadHistory()
		return
	}
	in.historyAt = -1
	in.clear()
}

// loadHistory replaces the line with the browsed history entry.
func (in *input) loadHistory() {
	in.runes = []rune(in.history[in.historyAt])
	in.cursor = len(in.runes)
	in.pendingDD = false
}

// take returns the line and resets the input for the next entry, returning
// to insert mode so the next keystroke edits rather than commands.
func (in *input) take() string {
	value := string(in.runes)
	in.clear()
	in.mode = inputInsert
	return value
}

// clear resets the line and the editing state (Ctrl+C when idle).
func (in *input) clear() {
	in.runes = in.runes[:0]
	in.cursor = 0
	in.historyAt = -1
	in.pendingDD = false
}

// render shows the value with a block cursor at the edit position.
func (in *input) render() string {
	if len(in.runes) == 0 {
		return "█"
	}
	return string(in.runes[:in.cursor]) + "█" + string(in.runes[in.cursor:])
}

package tui

// input is the shell's single-line input: typing, backspace, and cursor
// motion in insert mode. Vim normal-mode editing and command history arrive
// with the modeless-input ticket (#64); this is the minimal line the shell
// needs. It is only ever touched by the program loop, so it needs no locking.
type input struct {
	runes  []rune
	cursor int
}

// insert places r at the cursor and advances.
func (in *input) insert(r rune) {
	in.runes = append(in.runes, 0)
	copy(in.runes[in.cursor+1:], in.runes[in.cursor:])
	in.runes[in.cursor] = r
	in.cursor++
}

// backspace deletes the rune before the cursor.
func (in *input) backspace() {
	if in.cursor == 0 || len(in.runes) == 0 {
		return
	}
	in.runes = append(in.runes[:in.cursor-1], in.runes[in.cursor:]...)
	in.cursor--
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

// take returns the line and resets the input for the next entry.
func (in *input) take() string {
	value := string(in.runes)
	in.runes = in.runes[:0]
	in.cursor = 0
	return value
}

// render shows the value with a block cursor at the edit position.
func (in *input) render() string {
	if len(in.runes) == 0 {
		return "█"
	}
	return string(in.runes[:in.cursor]) + "█" + string(in.runes[in.cursor:])
}

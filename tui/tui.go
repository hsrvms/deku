// Package tui renders the Agent-emitted Activity Stream as a minimal
// full-screen terminal UI: a scrolling Transcript pane, a cumulative Turn Diff
// pane that auto-opens on a Turn's first Change, a status bar with the
// Working Indicator, and a single-line input. The TUI is a consumer of the
// activity seam (activity.Sink) and of the Agent's streamed output; it never
// derives or emits Turn state (ADR-0010). The Turn Diff is a display of Agent
// work — the renderer reads the working tree with git, never stages, stashes,
// or commits — and the Repository, Checkpoints, and Validation remain
// Agent-owned (CONTEXT.md: Turn Diff). It runs only on a real terminal (see
// Active); pipes, non-TTY output, TERM=dumb, and NO_COLOR keep the inline
// renderer unchanged as the fallback.
package tui

import "strings"

// Active reports whether the terminal UI should run instead of the inline
// renderer. The TUI requires a terminal, a TERM other than "dumb", and an
// unset NO_COLOR: every other environment keeps the inline renderer behavior
// unchanged — indicator transitions, Command Report, Tool Output, and
// refusal reporting (design guide §2 lists NO_COLOR among the fallback
// triggers, and the fallback is colorless, so NO_COLOR disables color).
func Active(terminal bool, term, noColor string) bool {
	return terminal && !strings.EqualFold(strings.TrimSpace(term), "dumb") && noColor == ""
}

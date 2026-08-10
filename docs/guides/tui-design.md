# Deku Terminal UI — Design Guide (v1)

**Status:** accepted design direction, precedes implementation. The v1 roadmap commits to a terminal UI whose design guide precedes implementation; this guide is that contract. The v1 specification builds on it; where the spec must still resolve details, this guide says so explicitly.

## 1. Principles

1. **Function over chrome** (ux > ui). Every pane and keybinding must earn its place by showing Agent activity or Agent work, not by looking like an app.
2. **The Agent is the sole source of Turn state** (ADR-0010). Every pane consumes the activity seam (`activity.Sink`: Working Indicator transitions and `Change` events); no renderer ever infers or derives Turn state.
3. **Modeless by default.** Typing always works; the Agent runs concurrently. There is no mode where the user "waits for the Agent" to type.
4. **Minimal surface.** Panes are views, not focus targets. The input line is always focused; no focus juggling, no modal system.

This guide explicitly rejects the pi-style full chrome — editor panes, modal command surfaces, and multi-pane focus management — while keeping the roadmap's "panes" as the minimal set below.

## 2. Interaction model

**Minimal full-screen pane TUI in a TTY** (alternate screen), with today's inline renderer unchanged as the fallback for pipes, non-TTY output, `TERM=dumb`, and `NO_COLOR`.

- The TUI deliberately gives up native shell scrollback inside the alternate screen; the scrollable Transcript pane replaces it (scroll bindings in §5).
- **Type while the Agent works:** the input line is always active. `Enter` while a Turn runs queues the message as the next Turn; `Ctrl+C` interrupts the running Turn (and clears the input line when idle).
- The non-TTY fallback keeps its v0.1 behavior: inline indicator transitions, Command Report, Tool Output, and refused-call reporting.

**Trade-off recorded:** option A (pure scrollback, no alternate screen) was rejected because a pinned input line is impossible while output streams — typing while the Agent works would interleave with the stream. The minimal TUI is the cheapest interaction model that satisfies that requirement.

## 3. Panes

```
┌ transcript (main) ─────────────────────────────────────────────────────┐
│  streaming conversation, scrollable; the Turn Diff block auto-opens    │
│  on a Turn's first Change inside the Agent's response section and      │
│  grows in place (Ctrl+T toggles the current Turn's block)              │
├────────────────────────────────────────────────────────────────────────┤
│  ● thinking · tool: read · provider/model     ← status bar             │
│  > _                                                                    │
└────────────────────────────────────────────────────────────────────────┘
```

The Turn Diff renders as a **typed block inside the Agent's response
section** of the Transcript — not a separate pane — so the Transcript stays
the single scrollable main area: the block appears at the first `Change`
event of a Turn, grows in place as the cumulative diff extends, and remains
in the Transcript as that Turn's history. The status bar and the input line
never move.

1. **Transcript pane** — the conversation; streams `TextDelta` output incrementally; scrollable in normal mode and with `Ctrl+E`/`Ctrl+Y` from any mode.
2. **Turn Diff block** — auto-opens on the first `Change` event of a Turn as a block inside the Agent's response section; shows the *cumulative* per-file working-tree diff of the Turn's Edits and Writes (never per-edit snapshots, so a second Edit to the same file extends the first, not replaces it); Writes of new files appear as new-file entries; the completed Turn's block stays in the Transcript until the next Turn begins — and remains there as history; Ctrl+T toggles the current Turn's block.
3. **Status bar** — the Working Indicator (label + glyph + color, triple redundancy per §6), the active Tool while working, and the current Provider/Model.
4. **Input line** — single-line, vim-mode, with command history; **Approval renders here**, not in a modal: the input line becomes the Command Report prompt and the status bar shows `awaiting-approval`, so the user approves the exact action (ADR-0009) without an overlay stealing focus.

## 4. Component conventions

- Each pane is a self-contained component with a fixed contract: it consumes activity events and renders only its own region. Panes never emit activity; the Agent remains the sole emitter (ADR-0010).
- Components use **semantic tokens only** (§6); raw colors are not allowed in component code.
- Shared components: the vim-mode single-line input (one implementation, used by input and Approval prompts) and a scrollable viewport wrapper (used by the Transcript, which hosts the Turn Diff block).
- Refused tool calls surface in the stream via the existing Tool Output machinery; the renderer must show the refusal reason, not drop it.

## 5. Keybinding policy

Rules (fixed):

1. **Discoverability:** `?` opens the keybinding help overlay (a normal-mode key — insert mode never traps a typeable character, rule 3); every binding is listed there.
2. **Vim orientation:** input editing follows vim normal/insert semantics.
3. **Modeless safety:** no binding may be a character that can be typed in insert mode. Bindings are normal-mode keys or `Ctrl` chords only.
4. **Stable reserved set:** `Enter`, `Ctrl+C`, `Ctrl+P` are reserved and documented; the table below is ratified by the v1 specification.

| Binding | Mode | Action |
|---|---|---|
| `Enter` | any | submit input; queue as next Turn while one runs |
| `Ctrl+C` | any | interrupt the running Turn; clear input when idle |
| `Ctrl+P` | any | open the Palette (roadmap-mandated shortcut) |
| `?` | normal | keybinding help overlay |
| `Ctrl+E` / `Ctrl+Y` | any | scroll Transcript down / up (vim-native scroll, works while typing) |
| `Ctrl+T` | any | toggle Turn Diff pane |
| `Ctrl+D` | any | quit the program (the inline path's EOF) |
| `Esc` / `i` / `a` / `A` / `I` | input | enter / leave insert mode |
| `h` `l` `0` `$` `w` `b` `x` `dd` | normal | vim movement and editing on the single-line input |
| `j` / `k` | normal | command history (shell-vi convention) |

The table is ratified by the v1 specification (`docs/specs/2026-08-09-v1-repository-intelligence-extension-delivery.md`), which resolves the `?` binding as a normal-mode key so that typing in insert mode never triggers it.

## 6. Color and accessibility baseline

- **Semantic tokens only:** `status.thinking`, `status.working`, `status.awaiting`, `diff.add`, `diff.del`, `input.prompt`, … Token palette is 16-color-safe with a 256-color / truecolor upgrade path; tokens are chosen for contrast on dark terminals.
- **Color is never the only signal:** the Working Indicator is label + glyph + color (e.g., `● thinking`, `▶ working`, `? awaiting approval`); the Turn Diff uses `+`/`-` prefixes, not hue alone.
- **Environment respect:** no color when not a TTY, under `NO_COLOR`, or `TERM=dumb`; the inline fallback renderer is color-safe and remains the non-TTY path.
- No state is conveyed through focus or cursor position alone; the status bar is always visible.

## 7. Rendering the Turn Diff

The renderer computes the Turn Diff from the activity seam: each `Change{Tool, Path}` event adds a path to the current Turn's set, and the pane renders the cumulative `git diff` of that set for the Turn so far. The seam needs **no new event types for v1** — the existing `Indicator` and `Change` events are sufficient. The Agent still owns the repository, Checkpoints, and Validation; the diff is a display of Agent work, not a correctness claim (CONTEXT.md: Turn Diff).

## 8. Library and dependency justification

The TUI is built on the charmbracelet stack: **bubbletea** (application model), **lipgloss** (styling), and **bubbles** (viewport component). This is Deku's first significant runtime dependency beyond the standard library, justified here per the repository dependency rules:

- **bubbletea** — Elm-style architecture with incremental rendering (only changed lines redraw, which streaming needs), first-class key parsing, alternate screen, and mouse handling, and the de facto standard for interactive Go CLIs.
- **lipgloss** — semantic, composable styling that maps directly onto the token baseline.
- **bubbles/viewport** — battle-tested scrolling for Transcript and Turn Diff.

Rejected alternatives: **tview** (whole-screen re-render per frame and a modal focus model, both wrong for live streaming and modeless input), **raw tcell** (would hand-build input, vim mode, and scrolling), **termui / gocui** (dashboard widgets / minimal maintenance).

## 9. What the v1 specification must still resolve

- Exact diff plumbing (per-file cumulative diff, truncation for large diffs).
- Approval prompt presentation in the input line (option keys, Command Report layout).
- Palette widget shape (filtering, grouping by Provider, current Selection marker).
- Ratification of the provisional binding table (§5).
- Non-TTY fallback parity checklist (what the inline renderer must still show).

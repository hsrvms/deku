# Terminal activity display: Working Indicator and live Turn Diff

**Status:** accepted

Deku surfaces Agent activity in the terminal. A **Working Indicator** shows whether the Model is thinking, a Tool is working, or the loop awaits Approval, so a silent Model call is not mistaken for a hang; a live **Turn Diff** shows the working-tree effect of Edits and Writes as they happen. Both are driven by the Agent, the only module that knows current Turn state. Deku builds the Agent-to-display activity seam now — the indicator transitions and change events the Agent emits — and renders the Working Indicator and Turn Diff as the v1 terminal UI rather than as a throwaway inline layer. A full TUI (panes, keyboard shortcuts such as a model palette, and a color and accessibility baseline) is v1 scope; Approval transparency is near-term and independent of it.

## Considered options

- Build a TUI now to host panes and keybindings. Rejected as a larger lift than v0 needs; the activity seam plus non-rendered display affordances cover the current need, and the TUI arrives in v1 where it belongs.
- Emit inline status and a printed diff now, then redraw the same information in a TUI later. Rejected to avoid building the inline renderer and then throwing it away; the seam is retained, the renderer is deferred.
- Derive status from Tool execution in the CLI. Rejected because the CLI is not the Turn orchestrator; the Agent owns state transitions and must emit the activity stream.

## Consequences

The Agent gains an activity-reporting surface (indicator transitions and change events) that any display consumes; the CLI remains a thin renderer. The Working Indicator and Turn Diff render as the v1 terminal UI on this seam rather than as an inline approximation. Approval transparency (ADR-0009) is near-term and does not depend on the TUI.
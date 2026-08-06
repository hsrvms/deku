# Approval transparency: Command Reports precede execution

**Status:** accepted

Approval must let the user see exactly what they are approving, and Tool output must be visible in the terminal. The Approval `Decider` currently receives only a Tool name and tier, so the prompt ("the command tool is destructive. Approve?") names a category but not the action, and Tool output is captured for the Model and never shown to the user. Deku changes the `Decider` seam so a Tool Call carries a human-readable **Command Report** — the exact command, the specific Edit, or the Write to a named path — and the Gate renders that Report in the prompt. Tool execution output is additionally echoed to the terminal regardless of whether the tool is destructive.

## Considered options

- Show the raw Tool name and tier (current behavior). Rejected because the user cannot infer specifics, defeating synchronous Approval as a genuine check.
- Return Tool output to the Model only. Rejected because the user remains blind to what ran on their machine.
- Have the Command Tool print its own report to the terminal. Rejected because it scatters display logic across Tools; the Report belongs at the Approval seam so every gated Tool benefits uniformly.

## Consequences

The `Decider` interface changes to carry the call context needed to render a Report; the Gate, the Agent's `runTool`, and the Tool definitions that produce Report text are updated together. This aligns implementation with the v0 spec's existing promise that "an explicit user-facing report always precedes execution." Because it widens the Approval seam, it is implemented as one slice across Approval, Agent, and Tool.
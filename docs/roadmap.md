# Product Roadmap

This roadmap describes the currently accepted product horizon. It is deliberately not a substitute for a specification: each milestone and material development must have a complete specification in [`specs/`](specs/) before implementation starts.

## v0 — Git-safe coding-agent foundation

A terminal-first Go coding agent for OpenAI-compatible providers. It establishes the Agent loop, built-in tools, exact-match Edits, approval, JSONL Sessions, a compact file-tree Repository Map, and opt-in Agent Commits. See the [v0 specification](specs/2026-08-02-v0-git-safe-coding-agent.md).

## v1 — Repository intelligence and extension delivery

Candidate scope, pending its own specification:

- MCP stdio Extension discovery, configuration, lifecycle, and tool bridging.
- An Anthropic Provider adapter.
- Tree-sitter Repository Maps and relevance ranking.
- Purpose-specific command experiences such as review, explain, and commit.

## Later horizon

The following ideas remain intentionally unscheduled until concrete use cases justify their complexity:

- Context Window summarization.
- Repository Memory.
- Agent-authored Extensions and their review or distribution workflow.
- TUI design guide: component conventions, keybinding policies, color and accessibility baseline.

## Documentation commitment

Deku updates the relevant specification, glossary, ADRs, configuration reference, and user documentation as part of each accepted change. Released behavior is documented as a support guarantee; roadmap items are not.

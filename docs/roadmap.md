# Product Roadmap

This roadmap describes the currently accepted product horizon. It is deliberately not a substitute for a specification: each milestone and material development must have a complete specification in [`specs/`](specs/) before implementation starts.

## v0 — Git-safe coding-agent foundation

A terminal-first Go coding agent for OpenAI-compatible providers. It establishes the Agent loop, built-in tools, exact-match Edits, approval, JSONL Sessions, a compact file-tree Repository Map, and opt-in Agent Commits. See the [v0 specification](specs/2026-08-02-v0-git-safe-coding-agent.md).

## Release and distribution

Accepted work for versioned CLI distribution is specified in [Release and CD publishing](specs/2026-08-03-release-cd-publishing.md). The first protected Release, `v0.0.2`, was published successfully, and the v0.1 milestone (configuration, Provider Registry and Selection, Approval transparency, activity seam) is released as `v0.1.0` through the same pipeline. The release workflow covers protected-tag Releases, supported-platform archives, checksums, provenance attestations, protected publication approval, and withdrawal or revocation guidance. Publishing a Release does not make the current pre-release implementation ready for daily use.

## v1 — Repository intelligence and extension delivery

Specified in [the v1 specification](specs/2026-08-09-v1-repository-intelligence-extension-delivery.md). Scope highlights:

- Extension discovery, configuration, and lifecycle: External Tools (commands declared in a JSON manifest) as the primary authoring path, and MCP stdio Tool bridging for ecosystem and stateful Tools.
- An Anthropic Messages Adapter: the second Adapter family, authenticated by API key. The OAuth subscription flow for native subscriptions (claude, codex) remains deferred per [ADR-0008](adr/0008-provider-registry-and-selection.md).
- Tree-sitter Repository Maps with symbol signatures, and truncation ranked for relevance to the current request within the token budget. Conversation-adaptive ranking is deferred to the later horizon.
- Purpose Command experiences such as review, explain, and commit: Commands that run the Agent as a Turn with a fixed purpose prompt and a purpose-scoped Tool set.
- Skills: user-authored instruction files (name, description, body) in `~/.deku/skills/` and trusted project `.deku/skills/`; the Agent loads a Skill when the request matches its description, and users can invoke one explicitly with `/skill:<name>`. Always-on system prompt instruction files join this work (ADR-0014): Global Instructions (`AGENTS.md` in the Deku Home), a System Prompt Override (`SYSTEM.md` in the Deku Home), and Project Instructions (`AGENTS.md` at the repository root, loaded regardless of Project Trust).
- A terminal UI: the Working Indicator and live Turn Diff rendered as panes, component conventions, keybinding policy (including the `/model` palette shortcut), and a color and accessibility baseline. Its design guide ([guides/tui-design.md](guides/tui-design.md)) precedes implementation.

The near-term display surface is deliberately limited to the Agent-to-display activity seam plus Approval transparency: a Working Indicator and Turn Diff render as the TUI in v1 rather than as a throwaway inline layer.

## Later horizon

The following ideas remain intentionally unscheduled until concrete use cases justify their complexity:

- Context Window summarization.
- Conversation-adaptive Repository Map ranking (whole-context relevance, as ADR-0001 originally predicted for v2).
- Repository Memory.
- Agent-authored Extensions and their review or distribution workflow.

## Documentation commitment

Deku updates the relevant specification, glossary, ADRs, configuration reference, and user documentation as part of each accepted change. Released behavior is documented as a support guarantee; roadmap items are not.

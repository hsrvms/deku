# v1: Repository Intelligence and Extension Delivery

**Status:** Ready for implementation

**Scope:** The v1 milestone: Extension delivery (External Tools and MCP Tools), the Anthropic Messages Adapter, tree-sitter Repository Maps with request-ranked truncation, Purpose Command experiences, Skills, and the terminal UI.

## Problem Statement

v0 and v0.1 delivered a terminal-first Go coding agent with one OpenAI-compatible Adapter, built-in Tools, exact-match Edits, Approval, JSONL Sessions, a file-tree Repository Map, opt-in Agent Commits, JSON configuration with a Provider Registry and runtime Selection, and an Agent-to-display activity seam. The agent is small and safe, but it cannot be extended, its Repository Map shows only file paths, it speaks one wire format, and its display surface is an inline renderer.

A developer who wants Deku to fit their workflow needs three things v0 cannot offer: a way to add capabilities without rebuilding Deku — a simple, script-based extension path with the MCP ecosystem as the advanced route; a Repository Map that shows symbols and adapts to the task within its token budget; and a terminal experience that shows live Agent work. They also need purpose-shaped interactions — review, explain, commit — and reusable instruction files (Skills) the Agent can consult on demand.

## Solution

v1 adds one **extension** module: a directory package (`~/.deku/extensions/<name>/` with a JSON `manifest.json` and `SYSTEM.md`), discovered at startup, enabled in the Deku Home settings module, contributing Tools of two kinds — **External Tools** (a declared command, spawned fresh per call) and **MCP Tools** (bridged from an MCP stdio server). Extension Tools register into the same Tool registry as built-ins, so Approval, Command Report, Tool Output, and Transcript machinery apply unchanged; undeclared Approval tiers default to Write.

The **provider** module gains a second Adapter family: **Anthropic Messages**, authenticated by API key, with the `Chat` interface unchanged. The **Repository Map** becomes tree-sitter based: symbol signatures for supported languages, ranked by term overlap with the current request when the repository exceeds the token budget, stable within a Turn. The CLI dispatches **Purpose Commands** (`/review`, `/explain`, `/commit`) that run the Agent as a Turn with a fixed purpose prompt and a purpose-scoped Tool set, and **Skills** (`~/.deku/skills/<name>/SKILL.md`, JSON front matter) that the Agent loads from a budgeted catalog when a request matches, or that the user invokes with `/skill:<name>`. The **terminal UI** renders the activity seam as panes — Transcript, cumulative Turn Diff, status bar with Working Indicator, and a vim-mode modeless input line — with the Approval prompt in the input area, per the design guide in `docs/guides/tui-design.md`.

**Acceptance statement:** with v1, a developer can drop a script-backed extension into `~/.deku/extensions/`, enable it in `settings.json`, and have the model call its Tool in a completed Turn with Approval, Command Report, Tool Output, and Transcript recording working exactly as for built-ins; can run `/review` and observe a Turn whose Tool set is read-only; can load a Skill automatically and explicitly; and can watch a Turn's cumulative diff live in the TUI with the Working Indicator showing thinking, working, and awaiting Approval. The non-TTY inline renderer keeps working unchanged.

## User Stories

### Extension delivery

1. As a developer, I want to install an extension by dropping a directory with a JSON manifest into `~/.deku/extensions/<name>/`, so that adding capabilities never requires rebuilding Deku.
2. As a developer, I want to enable an extension by listing its name in the Deku Home settings module, so that an installed but unlisted extension is inert.
3. As a developer, I want an External Tool — a Tool whose execution is a command declared in the manifest, in any language — so that a script is a complete extension Tool.
4. As a developer, I want each External Tool call to run in a fresh process with the call's arguments as JSON, returning the command's stdout as the Tool Result (structured when it parses as JSON, text otherwise), with stderr and exit code captured as error detail, so that a failing or crashing Tool is an ordinary failed Tool Result.
5. As a developer, I want an MCP Tool — a Tool bridged from a tool exposed by an MCP stdio server declared in the manifest — so that the existing MCP ecosystem works with Deku.
6. As a developer, I want the manifest to restrict which of an MCP server's Tools are bridged, so that a server that grows an unapproved Tool stays inert.
7. As a developer, I want the manifest to declare each extension Tool's Approval tier, with undeclared tiers defaulting to Write, so that unvetted extension code prompts before every execution.
8. As a developer, I want an extension's `SYSTEM.md` appended to the system prompt while the extension is enabled, so that the model knows when and how to use the extension's Tools.
9. As a developer, I want extension Tool Calls to flow through Approval, Command Report, Tool Output, and the Session Transcript exactly like built-in Tools, so that extensions are never a back door.

### Anthropic Messages Adapter

10. As a developer, I want an Anthropic Messages Adapter authenticated by API key, selectable as a Provider's Adapter family, so that I can run claude models through Anthropic's API or a compatible gateway.
11. As a developer, I want the Adapter to normalize Anthropic's streaming responses into the same Events as the OpenAI-compatible Adapter, so that the Agent loop is untouched by the new wire format.

### Repository Map intelligence

12. As a developer, I want the Repository Map to show symbol signatures (functions, types, methods) extracted with tree-sitter for Go, JavaScript/TypeScript, and Python, so that orientation is denser than a bare file tree.
13. As a developer, I want the map truncated within its token budget to keep the entries most relevant to the current request, so that a large repository still orients the model toward the task.
14. As a developer, I want the ranking stable within a Turn, so that the map does not change between Steps of one Turn.
15. As a developer, I want the map to keep its guarantees — always present, orientation only, never source code, never a constraint — with an explicit truncation note when entries are dropped.

### Purpose Command experiences

16. As a developer, I want `/review`, `/explain`, and `/commit` Commands, so that recurring purposes are a single request away.
17. As a developer, I want a Purpose Command to run the Agent as a Turn with a fixed purpose prompt and a purpose-scoped Tool set, so that it uses the normal machinery — Steps, Approval, Transcript — and cannot do more than its purpose allows.
18. As a developer, I want `/review` and `/explain` restricted to read-only Tools, so that a review or explanation can never mutate my repository.
19. As a developer, I want `/commit` to validate the Agent-owned changes since the last commit boundary and create an Agent Commit when they pass, without calling the model, so that committing is deterministic.
20. As a developer, I want a Purpose Command's failure modes reported like any Turn — interruption, failed Validation, external changes detected — so that recoverability rules are unchanged.

### Skills

21. As a developer, I want to author a Skill as a markdown file with a JSON front matter block (name, description) under `~/.deku/skills/<name>/`, so that instructions are plain files.
22. As a developer, I want the prompt to carry a compact catalog of Skill names and descriptions, bounded by a token budget and truncated with a note, so that the Agent knows what Skills exist without the prompt bloating.
23. As a developer, I want the Agent to read a Skill's body when the current request matches its description, so that relevant instructions apply without being always-on.
24. As a developer, I want to invoke a Skill explicitly with `/skill:<name>`, so that I can force instructions the Agent did not match.
25. As a developer, I want a project Skill of the same name to replace the global Skill, and duplicate names within one scope to fail fast at discovery naming the file, so that precedence is predictable.
26. As a developer, I want a Skill to carry instructions only, never Tools, so that Skills cannot become a second extension mechanism.

### Terminal UI

27. As a developer, I want a terminal UI in a TTY rendering the activity seam as panes — Transcript, cumulative Turn Diff, status bar, and input line — with the inline renderer unchanged as the non-TTY fallback, so that I can watch Agent work live without losing the pipe and CI path.
28. As a developer, I want to type while the Agent works: input is always active, `Enter` queues a message as the next Turn while one runs, and `Ctrl+C` interrupts the running Turn (clearing the input when idle), so that I never wait for the Agent to type.
29. As a developer, I want the Working Indicator in the status bar (thinking, working, awaiting Approval) with label, glyph, and color, so that the state is visible and never color-only.
30. As a developer, I want the Turn Diff pane to auto-open on the first change of a Turn and show the cumulative per-file working-tree diff of the Turn's Edits and Writes, so that I see the net effect, not per-edit snapshots.
31. As a developer, I want Approval to render in the input area as the Command Report prompt while the status bar shows awaiting Approval, so that I approve the exact action without an overlay stealing focus.
32. As a developer, I want vim-mode editing on the input line (Esc, i/a/A, h/l/0/$, w/b, x, dd, j/k history), so that editing matches my muscle memory.
33. As a developer, I want `Ctrl+P` to open the model Palette — an interactive, filterable list of Models grouped by Provider with the current Selection marked — so that switching Models is keyboard-driven.
34. As a developer, I want `?` to show the keybinding help, `Ctrl+E`/`Ctrl+Y` to scroll the Transcript from any mode, and the Turn Diff pane toggleable, so that the small binding set is discoverable.
35. As a developer, I want colors from semantic tokens only, `NO_COLOR` and `TERM=dumb` respected, and no state conveyed by color alone, so that the UI is accessible and environment-safe.

## Implementation Decisions

### Extension module

- The **extension** module owns discovery, validation, enablement, and Tool registration. Discovery scans `~/.deku/extensions/<name>/` at startup for `manifest.json`; a malformed manifest or an invalid Approval tier fails fast, naming the file and field. Enablement is the `extensions` section of the Deku Home `settings.json` (a list of names, replaced as a whole per Config Precedence); an installed but unlisted extension is inert. Project-scope extensions are not part of v1.
- The manifest declares, per External Tool: name, description, JSON Schema, Approval tier (default Write), and a command line with an optional timeout (default 120 seconds; the process is killed on expiry). Per MCP server: the server command line and an optional Tool allowlist — an absent allowlist bridges all of the server's Tools; a present one restricts them. Server Tool definitions come from `tools/list` at runtime; the manifest's allowlist is policy and wins.
- Execution: an External Tool call spawns the declared command in a fresh process with the JSON arguments on stdin; stdout is the Tool Result (parsed as JSON when it parses, text otherwise); stderr and the exit code are captured as error detail; a nonzero exit or crash is a failed Tool Result. MCP servers start lazily on the first bridged Tool Call of a Session and shut down when Deku exits; a crashed server fails the call and is restarted on the next call. Tool Calls, results, refusals, and output flow through the existing Approval, Command Report, Tool Output, and Transcript paths unchanged.
- Sequencing per ADR-0011: External Tools first, MCP bridging second; both are in this milestone.

### Provider module

- The **provider** registry gains an Anthropic Messages Adapter factory, selected by a Provider entry's Adapter family. The `Chat(ctx, model, system, messages, tools)` interface is unchanged; the Adapter translates Anthropic's streaming protocol — content blocks, `tool_use`, deltas, errors — into the existing Event types. Authentication is the existing API-key type resolved from `auth.json`; the OAuth subscription flow is out of scope.

### Repository Map

- The **repomap** module keeps the v0 contract — gitignore and exclusion policy, token budget (default 2000 tokens), truncation note, fresh build per Step — and gains tree-sitter symbol extraction for Go, JavaScript/TypeScript, and Python. Parsing results are cached per file mtime so repeated Builds do not reparse unchanged files. Unsupported languages render as plain file-tree entries.
- Ranking: when the built map exceeds the budget, entries are scored by term overlap between the current Turn's request and file paths, names, and symbol signatures; ties fall back to tree order; the lowest-ranked entries are dropped with the existing truncation note. The ranking is computed once per Turn — the request is constant within a Turn — so the map is stable across a Turn's Steps. The "map is not source code" and "map is not a constraint" prompt notes are unchanged.
- Tree-sitter is a CGO dependency; v1 accepts it. The Release pipeline builds the supported-platform archives with CGO enabled (per-platform toolchains), and the release runbook gains a note when v1 releases.

### Purpose Commands

- The **CLI** dispatches Purpose Commands before Turns, as it does `/model` today. The **Agent** owns purpose prompt assembly and Tool scoping: `/review` and `/explain` use a fixed purpose prompt and a read-only Tool set (filesystem read, search, Git inspection); no Edit, Write, or command Tools are registered for the Turn. `/commit` is mechanical: it runs Validation over the changes attributed to the Agent since the last commit boundary and creates an Agent Commit when they pass; it reports the outcome, refuses when external repository changes are detected, and reports when there is nothing to commit. Purpose Commands record in the Transcript like any Turn.

### Skills

- The **skills** module discovers `~/.deku/skills/<name>/SKILL.md` (and a trusted project's `.deku/skills/`) at startup and on trust grant. Each Skill is a markdown body with a JSON front matter block (`name`, `description`). A malformed front matter or a duplicate name within one scope fails fast naming the file; a project Skill replaces a same-named global Skill. The prompt carries a catalog of names and descriptions bounded by a token budget (default 500 tokens) and truncated with a note when it exceeds it; the catalog is built once per Session. The Agent reads Skill bodies with the existing `read` Tool when the request matches; `/skill:<name>` injects the named Skill's body into the next Turn. Skills carry no Tools, and v1 defines no interaction between Purpose Commands and Skills.

### Terminal UI

- The **tui** module renders the activity seam with bubbletea, lipgloss, and bubbles, per `docs/guides/tui-design.md`: a Transcript pane (incremental `TextDelta` rendering, scrollable), a Turn Diff pane (auto-opens on the first `Change` event of a Turn; shows the cumulative per-file working-tree diff computed by the renderer from the Change path set via `git diff`, with new files rendered as full-content addition entries and per-file (200 lines) and total (1000 lines) caps with a truncation note; persists until the next Turn), a status bar (Working Indicator with label, glyph, and color; active Tool; current Provider/Model), and a single-line vim-mode input. Approval renders in the input area as the Command Report prompt with the available decisions; the status bar shows awaiting Approval.
- The Transcript pane is a list of structured messages (kind + text) rendered from the activity seam, not raw streamed text: streamed model text stays plain and incremental; user requests render right-aligned in a semantic token color with a section separator at each exchange boundary; Tool Output and Command Report render as distinct styled blocks framed by separators. The pane drops the inline renderer's plain-text echo of a typed block (the Write immediately following the typed event, which the Agent emits first), so each block appears exactly once; the inline renderer is unchanged and still formats the same facts its own way.
- The Agent emits an explicit `idle` Working Indicator whenever a Turn completes — on success, failure, and interruption — so the status bar never claims thinking between Turns (ADR-0010: the Agent is the sole emitter of Turn state; the shell does not clear the indicator itself). Idle keeps glyph + label + color (`● idle`, gray), never color alone.
- The activity Sink gains typed display events: `ToolOutput(name, tier, content)`, emitted by the Agent at the point it echoes a Tool Result or a refusal, and `CommandReport(tool, tier, report)`, forwarded when a gated call's Command Report is rendered before an Approval decision is sought. The tier is the effective one under the Approval policy, matching what the gate displays; it is omitted when the Tool is unknown, as for a refused call to an undeclared Tool.
- The status bar's active Tool is part of the activity seam: the Sink gains `ActiveTool(name)`, emitted by the Agent at the moment a Tool begins executing, so no renderer derives which Tool is running from anything else. The shell also records the `Change` set the seam delivers; the Turn Diff pane renders it.
- Keybindings are ratified as in the design guide: `Enter` submit/queue, `Ctrl+C` interrupt/clear, `Ctrl+P` Palette, `?` help, `Ctrl+E`/`Ctrl+Y` scroll, `Ctrl+T` toggle Turn Diff, vim normal/insert editing with `j`/`k` history. The Palette is an interactive list of Models grouped by Provider with type-to-filter and the current Selection marked. Until the modeless-input ticket ratifies interrupt semantics, the TUI shell's `Ctrl+C` (and `Ctrl+D`, the inline path's EOF) quit the program; typing, mouse-wheel and PageUp/PageDown Transcript scrolling, and Enter submission or Approval-decision routing work from the shell ticket onward, with Approval decisions delivered to the waiting gate through a pipe so neither side touches the raw terminal.
- The TUI activates only when stdout is a TTY and `TERM` is not `dumb`; the inline renderer remains the non-TTY path with its v0.1 behavior (indicator transitions, Command Report, Tool Output, refusal reporting). `NO_COLOR` (present and non-empty) also falls back to the inline renderer, per the design guide's fallback list; the fallback is colorless, so `NO_COLOR` disables color as required. Colors come from semantic tokens only (16-color-safe ANSI numbers); no state is conveyed by color alone; the Working Indicator always renders glyph and label beside its color.

## Testing Decisions

- The primary seam remains the **Agent module** interface with a scripted Provider and a real temporary Git repository. Extension behavior is exercised through completed Turns: a fixture script registered as an External Tool (args on stdin, JSON and text results, nonzero exit, timeout kill), and a scripted MCP stdio server fixture (allowlist enforcement, crash → failed result → restart). Tests observe Approval with Command Reports, Tool Output, Transcript entries, and tier defaults.
- The **repomap** module is tested directly on deterministic fixtures: symbol extraction per language, cache invalidation on mtime change, request-ranked truncation (overlap scoring, tie-to-tree-order, stability across Steps of one Turn), and the truncation note. Unsupported languages fall back to tree entries.
- The **provider** module keeps its adapter-specific contract seam: the Anthropic Messages Adapter is tested against a controlled HTTP server speaking the Anthropic wire format — streaming Event normalization, `tool_use` translation, deltas, malformed responses, and errors.
- Purpose Commands are tested at the Agent seam: `/review` and `/explain` observe a read-only Tool set (a mutation attempt is refused by the registry, not by the model); `/commit` observes Validation, Agent Commit contents, refusal on external changes, and the nothing-to-commit report.
- Skills are tested through prompt assembly (catalog injection, budget truncation note), explicit `/skill:<name>` invocation, project-over-global replacement, and fail-fast discovery errors.
- The TUI is tested as bubbletea models with injected key events and in-memory sinks: pane state transitions, keybinding dispatch (queue on Enter while working, interrupt on `Ctrl+C`, Palette open/select/close, diff toggle), cumulative Turn Diff rendering from Change events, Approval prompt rendering in the input area, the idle indicator after a completed Turn, Tool Output and Command Report block rendering from injected events, user-message alignment and exchange/block separators, and the non-TTY fallback path (existing inline tests continue to pass). Tests assert rendered strings and state, not ANSI details.
- No test asserts private implementation details; all cases observe externally visible behavior through the seams above.

## Out of Scope

- OAuth subscription Authentication and the login/refresh lifecycle for claude and codex Providers.
- Conversation-adaptive Repository Map ranking (later horizon).
- MCP non-stdio transports, MCP resources and prompts, and MCP server installation or marketplace.
- Agent-authored Extensions and their review or distribution workflow.
- Project-scope Extensions (project `.deku/extensions/`).
- Skills distribution, sharing, or discovery beyond the local filesystem.
- Context Window summarization and Repository Memory.
- Themes and user-configurable color palettes; the v1 color baseline is the fixed semantic token set.

## Further Notes

- Governed by ADRs `0011-extension-tool-kinds` (superseding `0002`), `0012-repository-map-ranking` (partially superseding `0001`), `0013-skills`, `0010-terminal-activity-display`, `0009-approval-transparency`, and `0008-provider-registry-and-selection` (v1 scope note). The terminal UI is governed by the design guide `docs/guides/tui-design.md`, which `AGENTS.md` requires reading before TUI work.
- The settings module's `extensions` section and the extension manifest schema are resolved in this specification; the exact Anthropic-supported model compatibility matrix remains an implementation-level concern to document before release.
- The Release pipeline gains CGO-enabled builds when v1 releases; the release runbook must reflect that change at release time.
- Implementation order: Extension module (External Tools first, then MCP bridging), Repository Map intelligence, Skills and Purpose Commands, Anthropic Messages Adapter, then the terminal UI last, since it renders the seams the earlier items complete. This order is a constraint on sequencing, not a change of scope.

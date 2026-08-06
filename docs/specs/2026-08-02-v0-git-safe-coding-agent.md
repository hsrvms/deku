# v0: Git-Safe Coding-Agent Foundation

**Status:** Ready for implementation

**Scope:** Initial public release of Deku

## Problem Statement

Developers who primarily use OpenAI-compatible coding models through services such as OpenRouter, TokenRouter, and Qwen Cloud need a terminal-first coding agent that is small, understandable, and inexpensive in context use. Existing coding agents often depend on JavaScript or Python environments that bring heavy dependency trees and operational complexity.

The developer needs an agent that can orient itself in a repository without spending repeated model Steps on mechanical file discovery, make self-validating file changes, and preserve a recoverable Git workflow without silently incorporating the developer's unfinished work.

## Solution

Deku v0 is a single terminal-first Go application that runs a persisted Agent Session against one OpenAI-compatible Provider. For each Turn, the Agent injects a compact file-tree Repository Map, obtains model Tool Calls, enforces Approval, executes built-in tools, and continues through additional Steps until it can return a final response.

The Agent provides read, search, Edit, Write, command, and Git capabilities. Every Edit is an atomic exact-match operation, and Write creates or replaces whole files. Sessions are append-only JSONL logs. Git safety is opt-in: when Agent Commits are enabled, a clean repository receives an Agent Commit only after the completed Turn passes Validation. A dirty repository requires an explicit user choice to create a Checkpoint, stash existing work, continue with Agent Commits disabled, or cancel.

The v0 acceptance benchmark is: given a clean Go repository of approximately 30 source files with one seeded failing test, Deku identifies and fixes the root cause, passes `go test ./...`, and creates an Agent Commit using Qwen through an OpenAI-compatible Provider within eight Provider calls and 60,000 total billed input-plus-output tokens.

## User Stories

1. As a developer, I want to start `deku` in a repository and enter a natural-language request, so that I can use a coding agent without managing a separate runtime environment.
2. As a developer using an OpenAI-compatible routing service, I want to configure the Provider endpoint, credentials, and Model, so that I can use my preferred compatible coding model.
3. As a developer, I want Deku to show a clear startup error when required configuration is absent or invalid, so that I can correct it before a Turn begins.
4. As a developer, I want the Agent to receive a compact Repository Map automatically on each Step, so that it can find likely files without spending Tool Calls on trivial discovery.
5. As a developer, I want the Agent to read actual file contents before changing them, so that a Repository Map never becomes mistaken for source code.
6. As a developer, I want the Agent to list files and search repository text through Tools, so that it can investigate a problem without uncontrolled shell access.
7. As a developer, I want an Edit to succeed only when all specified old text exists exactly once, so that a stale or ambiguous request cannot partially mutate my repository.
8. As a developer, I want an unsuccessful Edit to leave every target file unchanged and report the mismatch to the Agent, so that the next Step can recover safely.
9. As a developer, I want the Agent to create a new file at a repository-relative path, including any missing parent directories, so that Deku can scaffold files it could not create before.
10. As a developer, I want a Write against an existing non-empty file to be refused unless an overwrite is requested, so that the model cannot accidentally clobber existing work.
11. As a developer, I want Read Tools to execute without a prompt and Write or Destructive Tools to require the appropriate Approval, so that routine inspection remains efficient without losing control of mutations.
12. As a developer, I want to approve or reject a requested mutation before it runs, so that I retain authority over my repository.
13. As a developer, I want every Session recorded append-only and resumable, so that a later Session can restore the conversation and Tool history without rewriting it.
14. As a developer, I want to configure Agent Commits as off, ask, or on, so that Git automation matches my risk tolerance.
15. As a developer who starts from a clean repository with Agent Commits enabled, I want Deku to create a commit containing only the Agent's validated changes from a completed Turn, so that I can review and revert discrete agent work.
16. As a developer who starts with a dirty repository, I want Deku to show staged, unstaged, and untracked work and ask whether to create a Checkpoint, stash it, continue without Agent Commits, or cancel, so that existing work is never silently committed or hidden.
17. As a developer, I want a stash choice to identify the precise stash created by Deku, so that later restoration does not rely on a mutable stash position.
18. As a developer, I want Deku to pause if external repository changes occur during a Turn, so that an Agent Commit cannot absorb another actor's work.
19. As a developer, I want incomplete work after interruption, Provider failure, budget exhaustion, or unsuccessful Validation to remain uncommitted, so that I can inspect it and choose whether to checkpoint or roll it back.
20. As a developer, I want the final response to distinguish Validation results from Git recoverability, so that a successful commit is not mistaken for proof that the repository works.
21. As a project maintainer, I want each accepted behavior change to update its specification and applicable public documentation, glossary terms, and ADRs, so that Deku remains understandable as an open-source project.
22. As a project maintainer, I want the v0 benchmark to record Provider calls and reported token usage, so that Repository Map and prompt changes can be evaluated against a stable token-efficiency target.

## Implementation Decisions

- The **Agent module** is the primary deep module. Its interface accepts one user request in a Session and drives the entire Turn: prompt assembly, Provider interaction, Tool execution, Approval, continuation Steps, Validation coordination, and final reporting. Callers do not coordinate model loops, Tool results, safety transitions, or commit decisions. The Agent drives multi-Step continuation for the built-in read-only Tools; later slices add the Repository Map, Approval, and mutation behavior.
- The Agent distinguishes a **Turn** from a **Step** as defined in `CONTEXT.md`. A Turn may make multiple Provider calls; the acceptance benchmark limits Provider calls, not Turns.
- Remaining v0 implementation follows this order: protect the Agent seam; complete the Session Transcript; deepen Tool execution; then deepen Repository safety. The Repository Map remains part of Prompt assembly inside the Agent and does not create a second orchestration path. This order constrains implementation without expanding v0 scope.
- The **Provider module** owns the existing `Chat(ctx, model, system, messages, tools)` interface and normalizes OpenAI-compatible streaming responses into Events. v0 supplies only the OpenAI-compatible adapter. The Anthropic adapter remains a separately specified future development.
- The **Tool module** owns model-visible definitions, argument validation, dispatch, and normalized results for built-in filesystem, repository search, Edit, command, and Git Tools. Native Tool Calls are the sole v0 action protocol; models that cannot reliably issue structured Tool Calls are unsupported in v0.
- The **Edit module** accepts a path plus one or more exact search-and-replace changes. It validates all matches for presence and uniqueness before mutating any file, providing all-or-nothing behavior for one Edit request.
- The **Write Tool** creates a new file, fills an empty file, or replaces a whole file's content at a repository-relative path, creating any missing parent directories. It is confined to the repository root, refuses to replace a non-empty file unless an overwrite is requested, writes atomically, and is classified as Write tier so it is gated by Approval. It complements the Edit Tool, which can only modify existing content.
- The **Command Tool** runs a shell command in the repository, capturing stdout, stderr, and the exit code for the Agent. It accepts an optional repository-relative working directory confined to the root and an optional timeout in whole seconds with a built-in default, and kills the process when the deadline or the caller context expires. It is classified as Destructive so it always requires Approval before any command executes.
- The **Approval module** owns the Read, Write, and Destructive classifications and pauses the Agent until the user decides. Per-tool and per-tier configuration may override the default classification.
- The **Prompt and Repository Map modules** assemble model input. v0 injects a compact file-tree Repository Map on every Step, regenerated fresh so each Step sees the current repository structure. The map respects `.gitignore` (including nested files and negation) and a configurable exclusion policy declared as `repo_map.exclude` gitignore-style globs in `~/.deku/config.yaml`. The map is bounded to a token budget (default 2000 tokens) and truncated with a note when it exceeds it; the `.git` directory is always excluded. The prompt must explicitly state that the map is not source code; the Agent uses `read` to obtain implementations. Tree-sitter parsing, symbol signatures, and relevance ranking are outside v0.
- The **Session module** persists an immutable, append-only JSONL Transcript under the Deku home directory and reconstructs the complete Transcript when resumed. Transcript records retain user messages, assistant responses, Tool Calls, and Tool Results in order. Session persistence remains independent of Provider wire types. The initial CLI creates timestamped unique Session IDs under `~/.deku/sessions/` and resumes an ID with `deku --resume <id>`.
- The **Repository module** is a concrete deep module that owns Git inspection, dirty-tree choices, Checkpoints, change snapshots, Validation outcome recording, and Agent Commit selection. No general Repository interface is introduced in v0 because there is one Git implementation and no demonstrated second adapter.
- Agent Commits are configured through `off`, `ask`, or `on`. With pre-existing repository changes, enabling Agent Commits requires an explicit Checkpoint, user-approved stash, choice to continue with commits disabled, or cancellation. Deku never stages all files indiscriminately and never commits or stashes pre-existing changes without approval.
- A successful Agent Commit follows one completed Turn whose Agent-owned changes passed Validation. Failed or interrupted work remains uncommitted. A commit is recoverability, not proof of correctness.
- v0 uses a single `deku` chat experience. Purpose-specific command experiences, planner, capability, memory, Extension loading, MCP process management, and Anthropic support are not part of v0.
- MCP stdio is the accepted Extension protocol for future work. Built-in Tools remain in the Deku process; an Extension delivery specification will define MCP lifecycle, discovery, configuration, failures, and permissions before implementation.
- Provider call count and reported input/output token usage are captured for the benchmark. A configurable runtime token-budget policy is outside v0; Providers that do not report sufficient usage cannot establish benchmark compliance.
- Documentation is part of each completed change. Public behavior updates require the relevant specification and user documentation; resolved terminology updates `CONTEXT.md`; qualifying architectural trade-offs receive an ADR.

## Testing Decisions

- The primary test seam is the **Agent module interface**. Deterministic integration tests use a scripted Provider adapter and a temporary Git repository to observe a complete Turn: Repository Map injection, Tool execution, Approval, complete Session Transcript entries, Edit outcomes, Validation reporting, change attribution, and Git state. This is the highest-value seam because it verifies the user-visible orchestration rather than internal loop mechanics.
- The Provider has a second, adapter-specific contract seam at its existing `Chat` interface. Contract tests use a controlled OpenAI-compatible HTTP server to verify streaming Event normalization, Tool Call arguments, Tool-result continuation, malformed responses, cancellation, and provider errors. A manually run compatibility suite validates intended Qwen routes before listing them as supported.
- The Repository module is exercised through the Agent seam using real temporary Git repositories, not mocks. Tests cover clean and dirty starts, Checkpoint approval, stash identity, disabled Agent Commits, cancellation, external change detection, Validation failure, interruption, and Agent Commit path selection.
- Edit behavior is verified through completed Turns: exact replacements succeed; missing or repeated old text fails with no target mutation; and multiple requested replacements are atomic.
- Write behavior is verified through completed Turns: new files and missing parent directories are created, empty files are filled, overwrite is refused without mutating the target, and a rejected Approval is reported as a denial.
- Session tests observe the complete persisted Transcript through resume behavior and ensure that appending new entries never mutates existing records. Tests include Tool Calls and Tool Results without depending on Provider wire types.
- Benchmark tests use a committed seeded Go fixture repository of roughly 30 source files with one failing test. They verify the outcome (`go test ./...` passes and an Agent Commit exists) and record Provider-call and token metrics. Benchmark claims require a real compatible Provider run; deterministic tests do not claim model quality or billed-token compliance.
- There are no prior code or test conventions in this greenfield repository. Tests will be written red-to-green in vertical slices, asserting externally observable behavior at the named seams rather than Tool internals or private implementation details.

## Out of Scope

- Anthropic Provider support.
- Extension discovery, MCP server lifecycle, Extension installation, and agent-authored Extensions.
- Tree-sitter Repository Maps, symbol signatures, and relevance ranking.
- Context Window summarization.
- Repository Memory, Planner, and Capability abstractions.
- Purpose-specific `review`, `explain`, `commit`, `index`, `extensions`, `provider`, `doctor`, `session`, `memory`, and `repo` command experiences.
- Textual or fenced-block Edit parsing as a fallback for models without reliable native Tool Calls.
- A configurable runtime token-budget enforcement policy.
- Universal Validation discovery for non-Go repositories.
- Non-Git repository workflows.

## Further Notes

- ADR-0001 defines the hybrid Agent-driven Repository Map approach.
- ADR-0002 reserves MCP stdio servers as the future Extension Tool protocol.
- ADR-0003 defines native Tool Calls and exact-match Edits.
- ADR-0004 defines opt-in Agent Commits and dirty-tree handling.
- ADR-0005 defines the deep-module ownership and implementation order for v0 Turn execution.
- The v0 benchmark is a product quality gate, not a general guarantee that every task completes in eight Provider calls or 60,000 tokens.
- The exact configuration schema, supported-model compatibility matrix, Repository Map exclusion policy, and user choice presentation require implementation-level design before their corresponding vertical slices. The Repository Map exclusion policy is resolved as `repo_map.exclude` gitignore-style globs in `~/.deku/config.yaml`; the remaining items must remain consistent with this specification and be documented when resolved.
- The Agent Commits configuration schema is resolved as `agent_commits.mode` (`off`, `ask`, or `on`, defaulting to `off`, also settable through the `DEKU_AGENT_COMMITS` environment variable) and `agent_commits.validation` (a shell command, defaulting to `go test ./...`) in `~/.deku/config.yaml`. The Agent Commit mode is parsed at startup and an invalid value fails fast. With Git safety active, the Agent inspects the repository at the start of the first Turn; a dirty repository requires an explicit user choice among Checkpoint, stash, continue without Agent Commits, or cancel. The Agent snapshots content hashes of tracked and untracked files at Turn start, attributes changed files to the Agent only when a mutating tool reported touching them, pauses on any external change, runs Validation, and stages Agent-owned files individually with `git add -- <path>` rather than `git add -A`.
- Future releases must receive their own specifications rather than treating roadmap candidates as committed requirements.

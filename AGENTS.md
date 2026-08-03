# Deku Engineering Guide

Deku is an open-source, terminal-first coding-agent platform written in Go. Its purpose is to give developers using OpenAI-compatible coding models a small, understandable, token-efficient agent with repository orientation and safe, recoverable Git workflows.

This file governs work in this repository. It supplements the repository's canonical product and architectural documents; when sources disagree, use the precedence below.

## Source of Truth and Precedence

1. Accepted ADRs in `docs/adr/` decide durable architectural trade-offs.
2. The active release specification in `docs/specs/` decides committed scope and externally observable behavior.
3. `CONTEXT.md` defines canonical domain vocabulary.
4. `docs/roadmap.md` describes candidate future work; it is not an implementation contract.
5. `docs/vision.md` describes enduring product intent; it is not a release specification.
6. This guide defines engineering workflow and code-organization constraints.

Before changing behavior, read `CONTEXT.md`, the active specification, and ADRs relevant to the affected module. Do not implement a roadmap item merely because it appears in the roadmap.

## v0 Architecture

The v0 specification is `docs/specs/2026-08-02-v0-git-safe-coding-agent.md`. Keep its scope small:

- One terminal-first `deku` chat experience.
- One OpenAI-compatible Provider.
- Built-in filesystem, search, Edit, command, and Git Tools.
- Approval, append-only JSONL Sessions, a compact file-tree Repository Map, Validation, and opt-in Agent Commits.

Do not add Anthropic support, MCP Extension loading, tree-sitter maps, Context Window summarization, Memory, Planner, Capability abstractions, or purpose-specific commands without a separately accepted specification.

### Module ownership

Organize code by the following domain modules, not generic technical layers:

- **Agent:** the primary deep module. It owns a complete Turn: prompt assembly, Provider Steps, Tool execution, Approval, continuation, Validation coordination, and final response. Callers submit a request and render results; they must not coordinate model loops or safety transitions.
- **Provider:** owns the `Chat(ctx, model, system, messages, tools)` interface and translates provider streaming responses into Events. v0 has one OpenAI-compatible adapter.
- **Tool:** owns model-visible Tool definitions, argument validation, dispatch, and normalized Tool results for built-in Tools. Tool Calls are the sole v0 action protocol.
- **Edit:** owns atomic exact-match replacement. Validate every match for presence and uniqueness before changing any file.
- **Approval:** owns Read, Write, and Destructive classifications and synchronous user decisions.
- **Prompt and Repository Map:** build input for every Step. The Repository Map is orientation only, never source code; the Agent must Read real content before an Edit.
- **Session:** owns the complete append-only JSONL Transcript and resume behavior. It does not depend on Provider wire types.
- **Repository:** owns Git inspection, dirty-tree choices, Checkpoints, snapshots, change attribution, Validation outcomes, and Agent Commit selection. Use real Git behavior; do not introduce a general Repository interface in v0 without a demonstrated second adapter.

Keep module interfaces small and deep. Add a seam only where behavior genuinely varies or where it is the agreed test seam. Prefer one high-value seam over multiple shallow abstractions.

### Deepening order

Implement the remaining v0 architecture in this order:

1. **Protect the Agent seam.** Make the Agent the sole owner of Turn and Step continuation before adding more Tool behavior.
2. **Complete the Session Transcript.** Persist user, assistant, Tool Call, and Tool Result history before Tool continuation depends on resume.
3. **Deepen Tool execution.** Keep definitions, argument validation, dispatch, and normalized results in Tool; keep Edit and Approval focused on their own domain rules.
4. **Deepen Repository safety.** Keep dirty-tree choices, Checkpoints, snapshots, Validation, external-change detection, and Agent Commit attribution in Repository.

The Repository Map remains a Prompt concern inside the Agent's Step assembly and does not create a second orchestration path. This order is an implementation constraint, not an expansion of v0 scope.

## Domain Rules

Use `CONTEXT.md` terminology exactly. In particular:

- A **Turn** is one user request and all work before its final response or interruption.
- A **Step** is one Model interaction and its resulting Tool work. A Turn may contain several Steps.
- An **Edit** is self-validating and atomic; ambiguity or a missing match means no mutation.
- A **Checkpoint** preserves pre-existing work before the Agent changes the repository.
- An **Agent Commit** contains only Agent-owned changes from one completed Turn that passed **Validation**.
- Validation detects failures. A commit provides recoverability; it does not prove correctness.

Do not silently commit, stash, stage, overwrite, or otherwise absorb pre-existing or externally introduced changes. Do not use `git add -A` for Agent Commits. Pause if external repository changes are detected during a Turn.

## Implementation and Testing

- Use the Go standard library before adding dependencies. Justify any new dependency in the relevant specification or ADR when it changes architecture.
- Keep infrastructure out of domain logic. The Agent depends on the Provider interface, never a provider-specific wire format.
- Validate all external input at module interfaces. Return contextual, explicit errors; never silently discard failures.
- Write tests red-to-green in vertical slices. Test externally observable behavior through agreed seams, not private implementation details.
- The primary v0 test seam is the Agent module interface, using scripted Provider behavior and real temporary Git repositories. Tests should observe a complete Turn: Repository Map injection, Tool execution, Approval, complete Session Transcript entries, Edit outcomes, Validation reporting, and Git state. The Provider `Chat` interface is the adapter-specific contract seam.
- Run `gofmt`, `go vet ./...`, static analysis configured by the repository, and affected tests before declaring work complete. Once the project has a Go module, run `go test ./...` unless the task makes that impractical, and report any skipped or failing checks.

## Documentation Is Part of Done

Deku is an open-source project. Documentation must remain accurate with implementation.

For every change, perform a documentation-impact review before completion:

1. Update the active spec in `docs/specs/` when committed scope, acceptance criteria, supported behavior, or an implementation decision changes. Do not rewrite historical decisions merely to match code; record a superseding decision where appropriate.
2. Update `CONTEXT.md` immediately when a domain term is introduced, resolved, or materially sharpened. Keep it a glossary: definitions only, no implementation details or plans.
3. Create an ADR in `docs/adr/` only when a decision is hard to reverse, surprising without context, and chosen among real alternatives.
4. Update user-facing documentation under `docs/guides/` or `docs/reference/` when a user-visible command, configuration option, workflow, compatibility guarantee, or safety behavior changes.
5. Update `docs/roadmap.md` when an item is accepted, released, deferred, or superseded. A roadmap item needs its own full spec before implementation begins.
6. Update `README.md` when the project status, supported installation path, or quick start changes.

Mention documentation files changed—or explicitly state why none were needed—in the final work summary.

## Git Discipline

- Never commit, push, or rewrite history unless the user explicitly asks.
- Do not discard user changes or perform destructive Git operations without explicit confirmation.
- Keep changes localized to the requested work. Avoid unrelated refactoring.
- Before handoff, show the changed files and summarize behavioral, architectural, and documentation effects.

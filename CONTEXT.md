# Domain Glossary

The canonical vocabulary for Deku. This file defines precise meanings for every term in the system. It is a glossary and nothing else — no implementation details, no specs, no plans.

---

## Agent

The core loop that mediates between the user, the model, and the tools. The Agent receives user input, assembles the system prompt, calls the model, parses the response, executes tool calls, and reports results. The Agent is the only module that orchestrates — it does not delegate orchestration to the model or to Tools.

**The Agent is not the model, not the user, and not a tool. It is the loop that connects them.**

---

## Turn

One user request and all work the Agent performs before its final response or an interruption ends the request. A Turn may contain multiple Steps, and a Session consists of many Turns.

---

## Step

One interaction with the Model within a Turn, including any tool requests it produces and their results. The Agent may take multiple Steps to complete one Turn.

---

## Session

A persisted conversation between the user and the Agent. A Session has a unique ID, a creation timestamp, and a full Transcript. Sessions are append-only and immutable — once a Transcript entry is written, it is never modified.

Sessions are stored as JSONL files in `~/.deku/sessions/`. Resuming a Session restores the full Transcript. The context window strategy determines what subset of the Transcript is sent to the model on each Step.

---

## Transcript

The ordered history of user requests, model responses, Tool Calls, and Tool results within a Session. A Transcript preserves the information needed to continue a Turn and resume a Session without losing work.

---

## Tool Call

A request from the Model for the Agent to execute a named Tool with structured arguments.

---

## Tool Result

The recorded outcome of a Tool Call, returned by the Agent to the Model and retained in the Transcript.

---

## Tool Output

The Tool Result content echoed to the terminal when a Tool Call executes — or is refused before execution, so the user sees why a call did not run — regardless of the Tool's tier, so the user sees what ran on their machine rather than only what the Model reports. It is distinct from the Command Report, which precedes execution; Tool Output follows it, and for a refused call it is what the user sees instead of an execution. The echoed block names the Tool and its effective tier; the tier is omitted when the Tool is unknown, as for a refused call to an undeclared Tool.

---

## Tool

A function the Agent can invoke on behalf of the model. Every tool has:

- A **definition** — a JSON Schema describing its name, description, and parameters. This is what the model sees.
- An **execution** — the behavior the Agent invokes when the model calls the tool.

Tools are the only way the model can affect the world outside its context window. Tools are not the model's subroutines — they are capabilities the Agent grants to the model.

**Built-in Tools** ship with Deku. **Extension Tools** are contributed by Extensions — either External Tools (a declared command) or MCP Tools (bridged from an MCP server) — and follow the same interface.

---

## Extension

A packaged set of Tools and a system prompt fragment that extends the Agent's capabilities. Each extension lives in a directory under `~/.deku/extensions/<name>/` and contains:

- `manifest.json` — name, version, description, dependencies, and the declaration of its Tools: External Tools with their commands, argument schemas, and Approval tiers, and MCP servers with their commands and optional Tool allowlists
- `SYSTEM.md` — appended to the system prompt when the extension is enabled; teaches the model when and how to use the extension's Tools

Extensions are **discovered** by scanning the filesystem and **enabled** by listing them in the Deku Home settings module (`settings.json`). An installed but unlisted extension is inert.

---

## External Tool

An Extension Tool whose execution is a command declared in the extension's manifest. When the Agent executes an External Tool, it runs that command in a fresh process and returns the command's output as the Tool Result; an External Tool therefore has no state between calls. The manifest declares the Tool's Approval tier; an undeclared tier defaults to Write, so an unvetted External Tool prompts before every execution.

---

## MCP Tool

An Extension Tool whose execution is delegated to a Tool exposed by an MCP server over stdio, as declared in the extension's manifest. The server supplies the Tool definitions; the manifest may restrict which of the server's Tools are bridged — a server that grows an unapproved Tool stays inert — and may declare their Approval tiers, which default to Write when undeclared.

---

## Adapter

A wire-format translator that converts the Agent's request and the Model's response into a specific model API's protocol. The Adapter interface is:

```
Chat(ctx, model, system, messages, tools) → stream of events
```

Deku's supported Adapter families are **OpenAI-compatible** and **Anthropic Messages**. The Agent loop is adapter-agnostic — it depends only on the interface, never on a specific wire protocol. An Adapter is constructed from a Provider's configuration; it is not itself the Provider.

---

## Provider

A named, configured model account the Agent can run against. A Provider declares an Adapter family, an optional base URL, its Authentication, and the Model Registry it exposes. Examples include tokenrouter, openrouter, claude, and codex. Subscription-based providers (claude, codex) authenticate with OAuth; custom providers (tokenrouter, openrouter, qwencloud) authenticate with a static API key.

The Provider is the selection unit: the Agent runs against one Provider and one Model at a time. The Provider is not the wire-format translator (that is the Adapter) and is not the intelligence (that is the Model).

---

## Authentication

The credential that lets a Provider be used. Authentication is typed: an **API key** (a static secret, often resolved from the environment) or **OAuth** (a token minted by a login flow that may expire and refresh). Each Provider has exactly one Authentication, stored separately from the Provider's Model Registry so secrets never travel with shared configuration.

---

## Model Registry

The set of Models a Provider exposes, declared per Provider. The Registry is what the Agent offers to the user when selecting a Model. A Model is addressed by name through its Provider.

---

## Selection

The choice of which Provider and Model the Agent uses for a Turn. Selection has a default (`defaultProvider` and `defaultModel`) and a per-Session override set by the `/model` Command. The override applies to subsequent Turns and is restored when the Session resumes; the default applies otherwise.

---

## Palette

The interactive Model selection surface of the terminal UI, opened by the model palette shortcut (`Ctrl+P`). Choosing a Model in the Palette sets the per-Session Selection override — the same effect as the `/model` command, in keyboard-driven form.

---

## Model

An LLM accessed through a Provider. The model is identified by name (e.g., `qwen-2.5-coder`, `claude-sonnet-4-20250514`). The model is the intelligence — the Agent is the infrastructure. The model decides what to do; the Agent executes and enforces.

---

## Approval

A safety gate that pauses the Agent loop before executing a tool and asks the user for confirmation. Tools are classified into three tiers:

- **Read** — auto-approved. Reading files, listing directories, git status, grep.
- **Write** — prompts the user. Editing files, creating files, git commit.
- **Destructive** — prompts the user with a warning. Deletion, force-push, commands with side effects.

Each tool declares its tier. The user can override per-tool or per-tier in configuration. Approval is synchronous — it blocks the Agent loop until the user responds.

---

## Command

User input beginning with `/` that invokes a named behavior instead of a normal chat Turn. Commands either act directly on Deku state — the `/model` Command changes Selection — or run the Agent with a fixed purpose (a Purpose Command). A Command is not a Command Report: a Command is user input; a Command Report is the description the user approves when a gated Tool Call is about to run.

---

## Purpose Command

A Command that runs the Agent as a Turn with a fixed purpose prompt and a purpose-appropriate Tool set. `review`, `explain`, and `commit` are the v1 examples. A Purpose Command begins a Turn and uses the normal Agent machinery — Steps, Approval, Transcript, Validation — with its Tool set scoped by its purpose, so a review does not Edit. The `commit` Purpose Command drives the creation of an Agent Commit or a Checkpoint; it is not itself the Git commit, which is the Agent Commit.

---

## Skill

A named instruction file that teaches the Agent how to perform a recurring task. A Skill is a markdown file with a JSON front matter block — the name and description — and a markdown body, living in `~/.deku/skills/<name>/` or a trusted project's `.deku/skills/`. Skills carry instructions only, never Tools. The Agent matches the current request against a catalog of Skill names and descriptions carried in the prompt and reads the Skill's body when it is relevant; the catalog is bounded by a token budget and truncated with a note when it exceeds it. The user may also invoke a Skill explicitly with the `/skill:<name>` Command. A project Skill of the same name replaces the global Skill. A Skill is not a Purpose Command: a Skill is user-authored content the Agent decides to use, while a Purpose Command is a product-defined experience the user invokes.

---

## Command Report

The user-facing description of what a gated Tool Call will do, shown at the point of Approval. A Command Report states the concrete action — the exact command, the specific Edit, or the Write to a named path — rather than just the Tool's name and tier, so the user approves an action, not a label. Because Approval gates on the Report, the user sees what will execute before it runs. A Command Report is not a Command: a Command is user input that invokes a behavior; a Command Report is the description shown at Approval.

---

## Context Window

The maximum number of tokens the model can process in a single request. The Agent manages the context window by assembling a prompt that fits within the limit. The management strategy is:

1. **Sliding window** — keep the most recent N tokens of the conversation log. Older messages are dropped.
2. **(Future) Summarization** — when the window fills, the oldest half of the conversation is summarized into a system message.

---

## Repository Map

A compact structural representation of the codebase, injected into the system prompt on every turn. The map shows file paths and symbol signatures (functions, types, methods) but not implementations. It gives the model orientation — "what exists and where" — without consuming the tokens that full file contents would require.

The repository map is produced automatically by the framework for each Step. The model does not invoke it as a tool. It is always present. The model still uses `read` to see actual file contents before editing.

The map is bounded by a token budget. When the repository exceeds the budget, entries are ranked for relevance to the current request and the lowest-ranked entries are dropped with a truncation note; ranking is stable within a Turn because the request is. Ranked truncation adapts the map to the task at hand without making it authoritative.

The map is not a constraint — the model can always explore files not shown in the map.

---

## Edit

A self-validating request to modify an **existing** file by replacing exact existing text with specified new text. An Edit cannot create a new file or populate an empty file, because an empty string is never a valid exact match. An Edit is accepted only when every requested match is present and unambiguous; otherwise, no change is made.

## Write

A Tool that creates a new file, fills an empty file, or replaces a whole file's content at a repository-relative path. Parent directories are created as needed. A Write against an existing non-empty file is refused unless the caller requests an overwrite. The Write Tool is distinct from the **Write** Approval tier: it is a capability, and because it mutates the repository it is classified as Write tier and therefore gated by synchronous Approval.

---

## Checkpoint

A user-approved Git commit that preserves existing work as a recoverable boundary before the Agent changes the repository.

---

## Validation

The assessment that changes satisfy the repository's applicable checks. Validation detects failures; it does not preserve work or provide rollback.

---

## Repository

The working tree and version history in which the Agent performs work. A Repository includes pre-existing work, Agent-owned changes, and the Git state used for Checkpoints, Validation, and Agent Commits.

---

## Agent Commit

A Git commit containing only changes attributed to the Agent during one successfully completed Turn whose changes passed Validation.

---

## Event

A single unit of output from a Provider during a model call. Events are typed: `TextDelta` (a fragment of model text), `ToolCall` (a complete tool invocation), `ToolCallDelta` (a fragment of a tool call being streamed), `Done` (end of response), `Error` (failure).

The Agent dispatches events to the display or to the tool execution buffer based on their type.

---

## Working Indicator

The visible state Deku shows while a Turn is in progress, so the user can tell what the Agent is doing. It distinguishes thinking (the Model has been called but produced nothing yet), working (a Tool is executing), and awaiting Approval (the loop is paused for a user decision). The indicator is driven by the Agent, the only module that knows the current Turn state.

---

## Turn Diff

The file changes a Turn introduces, surfaced to the user as they happen rather than only at the end. A Turn Diff lets the user see the working-tree effect of the Agent's Edits and Writes live. It is distinct from the Repository Map (orientation) and from Validation (assessment); it is a display of Agent work, not a correctness claim.

---

## Activity Stream

The Agent-to-display stream of Working Indicator transitions, active-Tool reports, and change events that Deku emits during a Turn. The Agent reports the Tool it is about to execute at the moment execution begins, so a renderer's status bar can name the active Tool while the Working indicator is showing. The Agent is the only module that knows current Turn state, so it is the sole source of the stream; any renderer (the v1 terminal UI in particular) consumes it to show a Working Indicator and a live Turn Diff. The stream is emitted by the Agent, never by the CLI or a Tool.

---

## Release

A named distribution of Deku tied to one Source Revision. A Release consists of Release Artifacts and Release Metadata; publication status does not change which source or artifact bytes belong to it.

---

## Release Version

The human-facing identifier used to refer to a Release. A Release Version is distinct from the Source Revision and from any individual Build.

---

## Source Revision

The exact, immutable state of Deku's source from which a Release is produced. All artifacts belonging to one Release come from the same Source Revision.

---

## Build

The production of an artifact from a Source Revision and declared build inputs. A Build produces an output; it is not itself a Release and does not, by itself, establish that the output is supported.

---

## Release Artifact

A file distributed to users as part of a Release, such as an executable or an archive for a Target Platform. Checksums, provenance, attestations, and Release Notes are Release Metadata rather than executable distribution artifacts.

---

## Target Platform

The operating-system and processor-architecture combination for which a Release Artifact is produced.

---

## Supported Platform

A Target Platform for which Deku publicly promises a usable Release Artifact and compatibility within the stated support boundary. Producing an artifact for a Target Platform does not make that platform supported.

---

## Release Metadata

Information published with a Release that describes or helps verify it, including checksums, provenance, attestations, and Release Notes.

---

## Checksum

A digest calculated from the exact bytes of a Release Artifact. A Checksum can detect accidental or incomplete transfer, but does not establish who produced the artifact or whether its contents are correct.

---

## Provenance

Information describing where and how a Release Artifact was produced, including its Source Revision and relevant build inputs. Provenance explains an artifact's origin; it is not by itself a guarantee of correctness or authenticity.

---

## Attestation

A verifiable assertion about a Release Artifact or its Provenance issued by an identified signer or authority. An Attestation demonstrates that the assertion was issued and has not been altered; it does not make unsupported claims in the assertion true.

---

## Release Notes

Human-readable information accompanying a Release that explains its user-relevant changes, limitations, and known concerns.

---

## Release Publication

The act of making a Release and its Release Artifacts available to users. Publication is distinct from Build and does not alter the identity or contents of the Release.

---

## Withdrawal

The removal of a published Release or Release Artifact from recommended or available distribution without changing the historical fact that it was published or altering copies already obtained by users.

---

## Revocation

A security declaration that a Release or Release Artifact must no longer be trusted. Revocation is stronger and more specific than ordinary Withdrawal because it communicates a loss of trust, not merely a distribution or maintenance decision.

---

## Deku Home

The single per-user directory that owns all of Deku's durable state and configuration, including the Session archive, the Authentication store, and the Provider Registry's non-secret configuration. Deku uses one Home directory rather than splitting configuration and data across platform-specific paths.

---

## Project Config

Configuration that lives inside a Repository and overrides the Deku Home configuration for that project. Project Config is loaded only after the user grants the project Trust. It is where a project agrees on shared behavior such as Approval policy and Repository Map exclusions.

---

## Config Precedence

The order in which configuration sources combine: built-in defaults, then the Deku Home global configuration, then Project Config, with values resolved from the environment winning over all of them. Each configuration section is replaced as a whole by the next higher-precedence source rather than merged field-by-field.

---

## Project Trust

The user's decision to load a Repository's Project Config and any project-local resources. Project Config is not loaded until the project is trusted, because it can change safety behavior such as Approval policy. Untrusted projects are ignored.

---

## Env Substitution

The rule that a configuration value may reference the environment instead of holding a literal. A value of the form `${VAR}` is replaced with the value of the environment variable VAR; `${VAR:-default}` supplies a fallback when VAR is unset or empty. A literal value always wins over a placeholder. An unset `${VAR}` with no default is a configuration error at startup that names the variable and the field it appears in, so misconfiguration fails fast — with one deliberate exception: an Authentication API key that does not resolve is left empty, leaving its Provider declared but unable to authenticate, so a missing secret never blocks the other Providers. The environment is a source of configuration values, not a separate precedence layer. Environment values are resolved from the real process environment first and, when absent, from the Deku Home `.env` file, so the process environment always wins over `.env`.

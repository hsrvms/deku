# Domain Glossary

The canonical vocabulary for Deku. This file defines precise meanings for every term in the system. It is a glossary and nothing else — no implementation details, no specs, no plans.

---

## Agent

The core loop that mediates between the user, the model, and the tools. The Agent receives user input, assembles the system prompt, calls the model, parses the response, executes tool calls, and reports results. The Agent is the only component that orchestrates — it does not delegate orchestration to the model or to tools.

**The Agent is not the model, not the user, and not a tool. It is the loop that connects them.**

---

## Turn

One user request and all work the Agent performs before its final response or an interruption ends the request. A Turn may contain multiple Steps, and a Session consists of many Turns.

---

## Step

One interaction with the Model within a Turn, including any tool requests it produces and their results. The Agent may take multiple Steps to complete one Turn.

---

## Session

A persisted conversation between the user and the Agent. A session has a unique ID, a creation timestamp, and a full message log (user messages, model responses, tool calls, tool results). Sessions are append-only and immutable — once a message is written, it is never modified.

Sessions are stored as JSONL files in `~/.deku/sessions/`. Resuming a session restores the full message log. The context window strategy determines what subset of the log is sent to the model on each Step.

---

## Tool

A function the Agent can invoke on behalf of the model. Every tool has:

- A **definition** — a JSON Schema describing its name, description, and parameters. This is what the model sees.
- An **execution** — the behavior the Agent invokes when the model calls the tool.

Tools are the only way the model can affect the world outside its context window. Tools are not the model's subroutines — they are capabilities the Agent grants to the model.

**Built-in tools** ship with Deku. **Extension tools** are contributed by extensions and follow the same interface.

---

## Extension

A packaged set of tools and a system prompt fragment that extends the Agent's capabilities. Each extension lives in a directory under `~/.deku/extensions/<name>/` and contains:

- `manifest.yaml` — name, version, description, tool list, dependencies
- `SYSTEM.md` — appended to the system prompt when the extension is enabled; teaches the model when and how to use the extension's tools

Extensions are **discovered** by scanning the filesystem and **enabled** by listing them in `config.yaml`. An installed but unlisted extension is inert.

---

## Provider

An adapter that translates the Agent's request into a specific model API's wire format. The Provider interface is:

```
Chat(ctx, model, system, messages, tools) → stream of events
```

Deku's initial Provider is **OpenAI-compatible**. Anthropic is a planned additional Provider. The Agent loop is provider-agnostic — it only depends on the interface, not on any specific API.

---

## Model

An LLM accessed through a Provider. The model is identified by name (e.g., `qwen-2.5-coder`, `claude-sonnet-4-20250514`). The model is the intelligence — the Agent is the infrastructure. The model decides what to do; the Agent executes and enforces.

---

## Approval

A safety gate that pauses the Agent loop before executing a tool and asks the user for confirmation. Tools are classified into three tiers:

- **Read** — auto-approved. Reading files, listing directories, git status, grep.
- **Write** — prompts the user. Editing files, creating files, git commit.
- **Destructive** — prompts the user with a warning. Deletion, force-push, commands with side effects.

Each tool declares its tier. The user can override per-tool or per-tier in `config.yaml`. Approval is synchronous — it blocks the Agent loop until the user responds.

---

## Context Window

The maximum number of tokens the model can process in a single request. The Agent manages the context window by assembling a prompt that fits within the limit. The management strategy is:

1. **Sliding window** — keep the most recent N tokens of the conversation log. Older messages are dropped.
2. **(Future) Summarization** — when the window fills, the oldest half of the conversation is summarized into a system message.

---

## Repository Map

A compact structural representation of the codebase, injected into the system prompt on every turn. The map shows file paths and symbol signatures (functions, types, methods) but not implementations. It gives the model orientation — "what exists and where" — without consuming the tokens that full file contents would require.

The repository map is produced automatically by the framework for each Step. The model does not invoke it as a tool. It is always present. The model still uses `read` to see actual file contents before editing.

The map is not a constraint — the model can always explore files not shown in the map.

---

## Edit

A self-validating request to change a file by replacing exact existing text with specified new text. An Edit is accepted only when every requested match is present and unambiguous; otherwise, no change is made.

---

## Checkpoint

A user-approved Git commit that preserves existing work as a recoverable boundary before the Agent changes the repository.

---

## Validation

The assessment that changes satisfy the repository's applicable checks. Validation detects failures; it does not preserve work or provide rollback.

---

## Agent Commit

A Git commit containing only changes attributed to the Agent during one successfully completed Turn whose changes passed Validation.

---

## Event

A single unit of output from a Provider during a model call. Events are typed: `TextDelta` (a fragment of model text), `ToolCall` (a complete tool invocation), `ToolCallDelta` (a fragment of a tool call being streamed), `Done` (end of response), `Error` (failure).

The Agent dispatches events to the display or to the tool execution buffer based on their type.
# Deku

> A minimal, terminal-first coding-agent platform written in Go.

Deku is being built for developers who use OpenAI-compatible coding models and want a small, understandable coding agent with repository orientation and safe, recoverable Git workflows.

## Status

Deku is in pre-release development. The current implementation target is [v0: Git-safe coding-agent foundation](docs/specs/2026-08-02-v0-git-safe-coding-agent.md). The first published distribution, [v0.0.2](https://github.com/hsrvms/deku/releases/tag/v0.0.2), is available for evaluation, but Deku is not yet ready for daily use.

The v0 acceptance benchmark is implemented as an opt-in integration test that runs a real, OpenAI-compatible Provider against a committed seeded Go fixture repository and records Provider-call and billed-token metrics. Run it with:

```sh
DEKU_BENCHMARK=1 go test ./agent/ -run TestV0Benchmark -v
```

It requires `DEKU_PROVIDER_ENDPOINT`, `DEKU_PROVIDER_API_KEY`, and `DEKU_PROVIDER_MODEL` to be configured, makes real Provider calls, and skips when `DEKU_BENCHMARK` is unset so ordinary test runs never claim model quality or billed-token compliance.

## Installation

Download the archive for your operating system and architecture from the [Releases](https://github.com/hsrvms/deku/releases) page. The initial release supports Linux amd64/arm64, macOS amd64/arm64, and Windows amd64. Each archive contains the executable at its root.

For example, to install the Linux amd64 build of v0.0.2:

```sh
VERSION=0.0.2
curl -LO "https://github.com/hsrvms/deku/releases/download/v${VERSION}/deku_${VERSION}_linux_amd64.tar.gz"
curl -LO "https://github.com/hsrvms/deku/releases/download/v${VERSION}/SHA256SUMS"
sha256sum --ignore-missing -c SHA256SUMS
tar -xzf "deku_${VERSION}_linux_amd64.tar.gz"
mkdir -p ~/.local/bin
install deku ~/.local/bin/deku
```

Use the platform's equivalent checksum and archive tools where `sha256sum`, `tar`, or `install` are unavailable. Confirm the installed version with `deku --version`.

## Principles

- Keep the framework small.
- Put intelligence in the Model.
- Put capabilities in Extensions.
- Use a Repository Map for orientation, not as a substitute for reading code.
- Make Edits exact-match and self-validating.
- Treat Validation and Git recoverability as separate guarantees.

## Documentation

- [Vision](docs/vision.md)
- [Roadmap](docs/roadmap.md)
- [Configuration reference](docs/reference/configuration.md)
- [Specifications](docs/specs/README.md)
- [Architecture decisions](docs/adr/)
- [Domain glossary](CONTEXT.md)
- [Engineering guide](AGENTS.md)

## Usage

Configure Deku with JSON files under the **Deku Home** directory
(`~/.deku/`). Configuration is split by risk into three optional modules — a
missing module is simply absent:

- `settings.json` — behavior: the default Selection, Approval overrides, Repository Map exclusions, Agent Commits.
- `auth.json` — credentials: named Authentication entries, kept apart from the Provider declaration so secrets never travel with shared configuration.
- `models.json` — the Provider Registry: named Providers, each declaring an Adapter family, a base URL, its Authentication by name, and the Models it exposes.

```sh
mkdir -p ~/.deku && cat > ~/.deku/models.json <<'EOF'
{
  "providers": {
    "openai": {
      "adapter": "openai-compatible",
      "base_url": "https://api.openai.com/v1",
      "auth": "openai",
      "models": ["gpt-4"]
    }
  }
}
EOF
cat > ~/.deku/auth.json <<'EOF'
{
  "openai": { "type": "api_key", "api_key": "your-api-key" }
}
EOF
cat > ~/.deku/settings.json <<'EOF'
{
  "defaultProvider": "openai",
  "defaultModel": "gpt-4"
}
EOF
```

Deku refuses to start when the Provider Registry is inconsistent — a Provider
that references an unknown Authentication, declares an unsupported Adapter
family, or omits its base URL or Models fails fast with an explicit error —
and when no Provider and Model are selected.

The full configuration reference — every option, its default, Config
Precedence, Env Substitution, Project Trust, the `/model` command, and a
complete defaulted example — is in
[`docs/reference/configuration.md`](docs/reference/configuration.md).

In brief: secrets and endpoints live in the Deku Home `.env` file or the
process environment and are referenced from the module files with Env
Substitution (`${VAR}` / `${VAR:-default}`); the active Provider/Model is a
**Selection** driven by `defaultProvider`/`defaultModel` and switchable at
runtime with `/model`; and a repository's own `.deku/` Project Config is
loaded only after you grant the project Trust.

### Provider selection

The active Provider and Model are a **Selection**: `defaultProvider` and
`defaultModel` from `settings.json`, overridden per Session by the `/model`
command. During a chat:

```
deku> /model
current selection: openai / gpt-4
openai: gpt-4, gpt-4o
deku> /model openai gpt-4o
selection: openai / gpt-4o
```

`/model` with no arguments lists the current Selection and every Provider the
Agent can authenticate to with its Models; `/model <provider> <model>`
switches the active Selection for subsequent Turns and records the override in
the Session, so it is restored when the Session resumes.

### Project Config and Project Trust

A Repository may carry project-scope configuration in the same three optional
modules under a `.deku/` directory at the repository top level:

- `.deku/settings.json` — behavior: the default Selection, Approval overrides, Repository Map exclusions, Agent Commits.
- `.deku/auth.json` — credentials: named Authentication entries.
- `.deku/models.json` — the Provider Registry: named Providers with their Adapter family, base URL, Authentication reference, and Models.

Project Config is loaded **only after you grant the project Trust**. When you
run Deku interactively in a repository that carries Project Config, Deku asks
whether to trust the project; a `yes` answer records the repository root in
`~/.deku/trusted_projects.json` and reloads configuration. Non-interactive runs
never prompt and never trust. An **untrusted repository is ignored entirely**:
its configuration files are never read, so they cannot change your Approval
policy or other settings. Deku reports the project scope at startup, so you
always know whether project-scope configuration is in effect.

Under Config Precedence, a trusted project's module **replaces** the Deku Home
module of the same name as a whole, rather than merging field-by-field: a
field the project module omits falls back to the built-in default, not to the
Deku Home value.

Start the interactive chat from a repository:

```sh
go run ./cmd/deku/
```

Deku prints the Session ID on startup. Sessions are stored as append-only JSONL
files under `~/.deku/sessions/` and can be resumed with:

```sh
go run ./cmd/deku/ --resume <session-id>
```

Print the embedded build version without loading Provider configuration:

```sh
go run ./cmd/deku/ --version
```

The chat experience supports multi-Step Turns with `command`, `ls`, `read`,
`grep`, `write`, and `edit` tools. Model text is streamed to the terminal as it
arrives; tool calls and results are retained in the append-only Session
transcript.

Before any Tool Call executes, Deku shows a **Command Report** of the concrete
action — the exact `command`, the specific Edit changes (rendered as a
green/red diff), or the `write` path — so you approve an action, not a Tool
name. Read-only tools (`ls`, `read`, `grep`) run without a prompt but still
show their Report. Mutating tools
(`write`, `edit`) pause and show the Report, then ask you to approve (`y`) or
reject (`n`) before running; a rejection is reported back to the model and the
tool does not execute. `write` creates a new file, fills an empty file, or—when
overwrite is requested—replaces a whole file's content; `edit` makes
exact-match replacements inside an existing file. The `command` tool runs a
shell command in the repository and is classified as Destructive, so it always
prompts with a warning before executing.

You can override the default classification in `~/.deku/settings.json`. Per-tool
tier overrides change how a named tool is classified, and per-tier defaults
change whether a tier runs unprompted (`auto`) or asks (`prompt`):

```json
{
  "approval": {
    "tools": { "edit": "destructive" },   // classify edit as Destructive
    "defaults": {                            // defaults are per-tier
      "read": "prompt",                      // ask even for read-only tools
      "write": "auto"                        // run all Write tools without asking
    }
  }
}
```

Deku injects a compact file-tree **Repository Map** into the system prompt on
every Step so the model can orient itself without spending tool calls on
mechanical file discovery. The map shows file paths, not source code; the model
must use `read` to obtain actual file contents before editing. The map is
generated fresh on each Step and its size is bounded to stay within a token
budget. It respects `.gitignore` and an additional exclusion policy declared in
`~/.deku/settings.json`:

```json
{
  "repo_map": {
    "exclude": ["vendor", "*.gen.go"]   // hide a whole directory and generated files
  }
}
```

## Git safety

Deku can preserve a recoverable, attributable Git workflow. Agent Commits are
opt-in and configured through `agent_commits.mode` in `~/.deku/settings.json`
(or the `DEKU_AGENT_COMMITS` environment variable):

- `off` (default) — never create Agent Commits.
- `ask` — ask before creating each Agent Commit after a completed Turn.
- `on` — create an Agent Commit automatically after each completed Turn.

```json
{
  "agent_commits": {
    "mode": "off",               // off | ask | on
    "validation": "go test ./..."  // command run before an Agent Commit
  }
}
```

With Agent Commits enabled, Deku inspects the repository at startup. A clean
repository with completed, validated work receives an Agent Commit containing
only the files the Agent changed; Deku stages each file individually and never
uses `git add -A`. A dirty repository asks whether to create a **Checkpoint**
(commit your existing work), **stash** it with an identifiable message, continue
without Agent Commits, or cancel, so pre-existing work is never silently
committed or hidden.

Validation runs after each completed Turn before any Agent Commit. If validation
fails, or the Turn is interrupted or the Provider fails, the Agent's work remains
uncommitted for you to inspect. If the repository changes externally during a
Turn, Deku pauses without committing. A successful commit is a recoverable
boundary, not proof that the repository is correct: Deku reports Validation
results separately from the commit it created.

## Development

Run the repository's complete local validation suite with:

```sh
make ci
```

This checks formatting, module integrity, static analysis, tests (including the
race detector), and the CLI build. Provider credentials are not required.

## License

A license has not yet been selected.

# Deku

> A minimal, terminal-first coding-agent platform written in Go.

Deku is being built for developers who use OpenAI-compatible coding models and want a small, understandable coding agent with repository orientation and safe, recoverable Git workflows.

## Status

Deku is in pre-release development. The current implementation target is [v0: Git-safe coding-agent foundation](docs/specs/2026-08-02-v0-git-safe-coding-agent.md). The first published distribution, [v0.0.2](https://github.com/hsrvms/deku/releases/tag/v0.0.2), is available for evaluation, but Deku is not yet ready for daily use.

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
- [Specifications](docs/specs/README.md)
- [Architecture decisions](docs/adr/)
- [Domain glossary](CONTEXT.md)
- [Engineering guide](AGENTS.md)

## Usage

Configure an OpenAI-compatible Provider with environment variables or
`~/.deku/config.yaml`:

```sh
export DEKU_PROVIDER_ENDPOINT=https://api.openai.com/v1
export DEKU_PROVIDER_API_KEY=your-api-key
export DEKU_PROVIDER_MODEL=your-model
```

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

Read-only tools (`ls`, `read`, `grep`) run without a prompt. Mutating tools
(`write`, `edit`) pause and ask you to approve (`y`) or reject (`n`) before
running; a rejection is reported back to the model and the tool does not
negotiate. `write` creates a new file, fills an empty file, or—when overwrite is
requested—replaces a whole file's content; `edit` makes exact-match replacements
inside an existing file. The `command` tool runs a shell command in the
repository and is classified as Destructive, so it always prompts with a
warning before executing.

You can override the default classification in `~/.deku/config.yaml`. Per-tool
tier overrides change how a named tool is classified, and per-tier defaults
change whether a tier runs unprompted (`auto`) or asks (`prompt`):

```yaml
approval:
  tools:
    edit: destructive   # classify edit as Destructive
  defaults:
    read: prompt        # ask even for read-only tools
    write: auto         # run all Write tools without asking
```

Deku injects a compact file-tree **Repository Map** into the system prompt on
every Step so the model can orient itself without spending tool calls on
mechanical file discovery. The map shows file paths, not source code; the model
must use `read` to obtain actual file contents before editing. The map is
generated fresh on each Step and its size is bounded to stay within a token
budget. It respects `.gitignore` and an additional exclusion policy declared in
`~/.deku/config.yaml`:

```yaml
repo_map:
  exclude:
    - "vendor"        # hide a whole directory
    - "*.gen.go"      # hide generated files
```

## Development

Run the repository's complete local validation suite with:

```sh
make ci
```

This checks formatting, module integrity, static analysis, tests (including the
race detector), and the CLI build. Provider credentials are not required.

## License

A license has not yet been selected.

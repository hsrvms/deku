# Deku

> A minimal, terminal-first coding-agent platform written in Go.

Deku is being built for developers who use OpenAI-compatible coding models and want a small, understandable coding agent with repository orientation and safe, recoverable Git workflows.

## Status

Deku is in pre-release development. The current implementation target is [v0: Git-safe coding-agent foundation](docs/specs/2026-08-02-v0-git-safe-coding-agent.md). It is not yet ready for installation or daily use.

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

The initial chat experience is a single-Step Turn without tools. Model text is
streamed to the terminal as it arrives.

## License

A license has not yet been selected.

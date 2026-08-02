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

## License

A license has not yet been selected.

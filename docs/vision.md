# Vision

## Deku

Deku is a minimal, extensible coding-agent platform written in Go.

The name comes from *deku* (木偶), historically a wooden puppet or figure that performs work on behalf of another. Here it means a humble, dependable worker that carries out work under the developer's direction—not an autonomous engineer.

## Product intent

Deku is a terminal-first AI-powered software-engineering platform that combines:

- Pi's minimal agent architecture.
- Aider-inspired repository intelligence and Git recoverability.
- Go's simplicity, concurrency, and portability.
- A first-class extension ecosystem.
- A future path toward agent-authored Extensions.

Deku should be suitable as a daily coding assistant while remaining understandable enough for one developer to comprehend its architecture.

> The framework should remain small. Intelligence belongs in the Model. Capabilities belong in Extensions.

## Scope discipline

This vision is durable intent, not an implementation contract. The [roadmap](roadmap.md) describes the accepted product horizon, and every release or material development must have a complete specification in [`docs/specs/`](specs/) before implementation begins.

The initial implementation scope is defined by the [v0 Git-safe coding-agent foundation specification](specs/2026-08-02-v0-git-safe-coding-agent.md).

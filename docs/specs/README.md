# Specifications

Deku treats specifications as maintained project documentation, not disposable planning notes. Every release milestone and material future development must have a dated specification in this directory before implementation begins.

## Lifecycle

1. Create a specification from the accepted product and architectural decisions.
2. Keep it current while the work is implemented; record scope changes, accepted trade-offs, and verification results.
3. Update `CONTEXT.md` immediately when domain vocabulary is resolved.
4. Create an ADR in `docs/adr/` for a hard-to-reverse decision that is surprising without context and reflects a real trade-off.
5. Update public documentation when a user-visible interface, configuration option, workflow, or support guarantee changes.
6. Mark the specification's implementation status when the work is released or superseded; do not rewrite the original decision history.

## Version planning

`docs/roadmap.md` records the current product horizon. A roadmap item is not implementation-ready on its own: it must receive its own complete specification before work starts. This prevents the project from inventing detailed requirements for future releases before their constraints and user needs are known.

## Current specifications

- [v0: Git-safe coding-agent foundation](2026-08-02-v0-git-safe-coding-agent.md)
- [Release and CD publishing](2026-08-03-release-cd-publishing.md)
- [v0.1: configuration, providers, approval transparency](2026-08-06-v0-1-config-providers-approval.md)

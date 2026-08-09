# Hybrid architecture: agent-driven loop with automatic repository map injection

Deku uses an agent-driven architecture (the model decides what to read and edit by calling tools) but also injects a repository map into the system prompt on every turn. The map gives the model structural orientation without constraining it — the model can always explore beyond the map. This is a deliberate hybrid that avoids the token waste of pure exploration while preserving the flexibility of agent-driven control.

## Status

Accepted.

**Partially superseded by [0012-repository-map-ranking.md](0012-repository-map-ranking.md):** the token budget shipped in v0; tree-sitter parsing and request-ranked truncation land in v1; the conversation-adaptive ranking this ADR predicted for v2 is deferred to the later horizon.

## Considered Options

### Pure agent-driven (no map)

The model sees no pre-computed codebase context. Every file exploration requires a tool call: `ls` → `ls src/auth/` → `read src/auth/login.go`. Three turns to reach the first relevant file.

**Rejected because:** Wastes tokens and turns on mechanical discovery. The model spends 30-50% of its context budget just finding the right files. For large codebases, the model gets lost.

### Pure framework-driven (map replaces exploration)

The framework pre-computes what's relevant and injects full file contents. The model never calls `read` — it only edits.

**Rejected because:** The framework can't know what the model will need. It misses surprising cross-file connections. The model can't explore — it's trapped in whatever the framework decided was relevant. This violates the "intelligence belongs in the model" principle.

### Hybrid (map + tools)

The framework injects a compact structural map (file paths, symbol signatures, no implementations). The model uses the map to decide what to read, then calls `read` to get actual code. The map is always present but never constraining.

**Chosen.** The map eliminates the wasteful discovery turns. The model still reads files before editing. The model can still explore files not in the map.

## Consequences

- The repository map must be produced on every turn. This requires tree-sitter parsing (v1) and relevance ranking (v2). A simple file tree is the v0 starting point.
- The map competes for context window space with the conversation history. It must be kept compact — ≤2K tokens in v2.
- The "Note: this is a map, not actual code" instruction in the system prompt is critical. Without it, models sometimes hallucinate edits against the map.
- If the map is wrong or stale (e.g., the user just created a file), the model can still discover it via `ls`. The map is a hint, not a contract.
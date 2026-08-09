# Repository Map ranking: request relevance in v1, conversation-adaptive later

**Status:** accepted

**Partially supersedes:** [0001-hybrid-agent-repo-map.md](0001-hybrid-agent-repo-map.md)

ADR-0001 predicted tree-sitter parsing in v1 and relevance ranking in v2, with the token budget arriving in v2. Implementation crossed that timeline: the token budget (2000 tokens default) and truncation shipped in v0 — and v0 truncation is arbitrary, cutting the map off in tree order. Ranking is therefore not a new mechanism but the missing half of an already-shipped budget mechanism, and it belongs with the tree-sitter work in v1.

"Relevance ranking" hid two distinct mechanisms, resolved as follows:

- **Request relevance (v1).** When the repository exceeds the budget, entries are ranked by term overlap with the current Turn's request (paths, names, symbol signatures), and the lowest-ranked entries are dropped with a truncation note. Properties: cheap and deterministic (no model involved); stable within a Turn because the request is, so the map does not thrash between Steps; and it degrades to tree order when the request has no discriminating terms — never worse than today's arbitrary cutoff. The "map is not a constraint" guarantee (ADR-0001) is unchanged.
- **Conversation-adaptive ranking (later horizon).** Scoring against the whole Session context, as ADR-0001 originally envisioned for v2. Deferred because it re-ranks as the conversation grows (thrash and cost) and its quality bar is a separate problem from tree-sitter extraction. Request relevance covers the dominant case; cross-Turn references ("now do the same for the admin panel") are its known limitation and the reason this form stays on the horizon.

## Considered options

- **Both mechanisms in v1.** Rejected: conversation-adaptive ranking is a tuning-heavy quality problem that would delay tree-sitter extraction, and its per-Step re-ranking conflicts with the map's orientation role.
- **No ranking in v1** (ADR-0001's original timeline). Rejected: the budget already shipped in v0, so v1 would keep arbitrary truncation while adding richer symbol content that is more misleading when cut arbitrarily.
- **Request relevance only, indefinitely.** Rejected: cross-Turn references are a real use case; the later horizon keeps the door open.

## Consequences

The v1 Repository Map shows symbol signatures for the files that fit the budget, ranked by request relevance. Tree-sitter Go bindings bring a CGO dependency, which the v1 specification must weigh against the cross-compiled Release pipeline; the v1 language set starts with Go plus a small set (JavaScript/TypeScript, Python). The roadmap's v1 item is reworded to request-ranked truncation, and conversation-adaptive ranking is listed in the later horizon.

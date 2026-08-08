# v0.1 Development Plan — config, providers, approval transparency, activity seam

**Status:** completed — all four phases delivered as `v0.1.0`
**Spec:** `2026-08-06-v0-1-config-providers-approval.md`
**ADRs:** 0007, 0008, 0009, 0010 (seam)

The plan sequences four interdependent phases. Each phase is red-to-green at an agreed seam, lands behind the existing Agent-seam test convention, and updates the relevant documentation before it is considered done.

## Phase 1 — Config foundation (ADR 0007)

Replace the YAML loader with JSON configuration and the precedence model.

- **Scope:** JSON files under Deku Home; optional Project Config; Config Precedence (`defaults < global < project < environment-as-source`); Env Substitution (`${VAR}` / `${VAR:-default}`); replace-per-section merge; Project Trust gate; Deku Home `.env` loading with real env winning.
- **Seam:** `config.Load` with controlled Deku Home and environment. Table-driven cases for precedence, substitution, missing-placeholder errors, section replacement, and the Trust gate.
- **Done when:** the loader produces a `Config` with no Provider wiring yet; existing tests ported; docs (`README`, config reference) updated; spec user stories 1–10 pass.

### Phase 1a — JSON configuration foundation (issue #34)

Delivered by issue #34. The YAML loader is replaced by a JSON loader under Deku
Home (`~/.deku/config.json`) that resolves every value in Config Precedence
order `defaults < global < environment-as-source`, with Env Substitution
(`${VAR}` / `${VAR:-default}`), a literal value overriding an environment
placeholder, and a fast failure when a required value is missing or an unset
placeholder has no default. The `provider.endpoint`, `provider.api_key`, and
`provider.model` fields remain required; the `DEKU_PROVIDER_*` and
`DEKU_AGENT_COMMITS` environment variables remain the environment-as-source
layer. Existing config tests are ported from YAML to JSON.

**Deferred from Phase 1 (not part of #34):** Project Config and the Project
Trust gate, Deku Home `.env` auto-loading, and splitting configuration into
`settings.json` / `auth.json` / `models.json` (that split accompanies the
Provider Registry in Phase 2). User stories 6, 7, and 9 are therefore not yet
fully satisfied.

**Delivered by issue #36:** the modular split into `settings.json` /
`auth.json` / `models.json` and the Deku Home `.env` auto-loading with the
real process environment winning, satisfying user stories 6, 9, and 10 at the
module granularity.

**Delivered by issue #38:** Project Config and the Project Trust gate,
completing the section-replacement rule between file sources and satisfying
user stories 5 and 7. Project Config uses the same three optional modules
(`settings.json` / `auth.json` / `models.json`) under a `.deku/` directory at
the repository top level, located through the Git top level. Project modules
are loaded only when the repository's root is listed in the Deku Home trust
record (`~/.deku/trusted_projects.json`); the decision is a deterministic
exact-path match, an absent record trusts nothing, and an untrusted
project's files are never read. A trusted project's module replaces the Deku
Home module of the same name as a whole under Config Precedence
(`defaults < global < project < environment-as-source`). Trust is granted
interactively: when a repository carries Project Config and input is a
terminal, the CLI asks whether to trust the project and records a yes answer
in the trust record automatically (`config.GrantTrust`) before reloading
configuration; non-interactive runs never prompt and never trust.

## Phase 2 — Provider registry and Selection (ADR 0008)

Introduce the Adapter/Provider split and runtime selection.

- **Scope:** Adapter (unchanged `Chat` interface) vs ProviderRegistry; a factory that builds the correct Adapter from a Provider entry; Authentication resolved by Provider name; `defaultProvider`/`defaultModel`; a per-Session Selection override; `/model` command dispatch in the CLI (command-first; the palette shortcut is v1).
- **Seam:** Agent module for end-to-end Selection; provider registry factory for adapter construction. Custom (URL+key) Providers only — native subscriptions are out of scope.
- **Done when:** multiple custom Providers configure and run; `/model` switches the active Provider+Model between Turns and persists on resume; errors surface for missing/blank Selection; spec user stories 11–18 pass.

**Delivered by issue #39:** the Provider Registry and Adapter factory. `models.json`
now declares named Providers (Adapter family, base URL, Authentication by
name, Models) and `auth.json` named Authentication entries, replacing the
former single-provider `endpoint`/`model`/`api_key` fields and the
`DEKU_PROVIDER_*` environment variables. `provider.NewRegistry` validates every
entry at construction — an unsupported Adapter family, an unknown or
unsupported Authentication, a missing base URL, or an empty Model Registry
fails explicitly — and `Resolve` builds the correct Adapter for a Selection.
An Authentication whose key does not resolve leaves its Provider declared but
unable to authenticate, so a missing secret never blocks the other Providers.

**Delivered by issue #41:** Selection and the `/model` command.
`defaultProvider`/`defaultModel` in `settings.json` provide the default
Selection; the CLI resolves the per-Session override recorded in the Session
transcript over it and reports explicitly when no Provider or Model is
selected, when the selected Provider is unknown, or when it cannot
authenticate. `/model` lists only Providers the Agent can authenticate to with
their Models; `/model <provider> <model>` switches the active Selection
between Turns through `agent.SetSelection` and records the override in the
Session, so it is restored on `--resume`. User stories 11–18 pass; the
environment remains a value source through `${VAR}` substitution and the Deku
Home `.env` file.

## Phase 3 — Approval transparency (ADR 0009)

Make gated actions visible before execution.

- **Scope:** `Decider` seam carries a **Command Report**; the Gate renders it in the prompt; each built-in Tool produces Report text (exact command, Edit changes, Write path); Tool output echoed to the terminal regardless of tier; a call whose Report cannot be rendered is refused.
- **Seam:** approval `Gate` with in-memory reader/writer; Agent seam for end-to-end Transparency and Session recording of denials.
- **Done when:** a gated command/Edit/Write shows its concrete action before the y/n prompt; tool output is visible; spec user stories 19–22 pass.

**Delivered by issue #40:** Tool execution output is echoed to the terminal
regardless of the Tool's tier — auto-approved Read Tools and prompted
Write/Destructive Tools alike — framed as a `Tool output (<tool>, <tier>):`
block with the indented normalized result. A rejected Tool Call is reported
to the user with an explicit notice, recorded in the Session transcript as a
denial Tool Result without executing, and never re-asks the same Approval
decision: each call receives exactly one decision and the Turn continues to
the next Step with the denial as the Tool Result. User stories 19–22 pass.

## Phase 4 — Activity seam (ADR 0010, seam only)

Establish the Agent-to-display activity stream for the future TUI.

- **Scope:** the Agent emits Working Indicator transitions (thinking / working / awaiting Approval) and change events (Edit/Write) to an activity sink interface. No renderer in scope — the CLI remains line-based; the v1 TUI renders these.
- **Seam:** Agent module — a fake activity sink observes the emitted stream across a complete Turn.
- **Done when:** a commented-through Turn produces a deterministic indicator+change sequence; spec user stories 23–25 pass.

## Cross-cutting discipline

- **Deepen, don't scatter:** each phase keeps the Agent as the sole Turn orchestrator; the CLI stays a thin renderer. No new shallow orchestration paths.
- **Seam count:** one primary seam (Agent) plus focused seams for config, approval, and the registry factory. Prefer existing seams; add none beyond what the ADRs justify.
- **Docs with code:** each phase updates the relevant ADR-referenced behavior in `docs/specs/`, `CONTEXT.md` (if a term sharpens), `README.md`, and the config/provider reference before claiming completeness.
- **Exit checks per phase:** `gofmt`, `go vet ./...`, static analysis configured by the repo, and `go test ./...` pass; broken or skipped checks are reported, never hidden.

## Risks

- **Breaking config change (Phase 1):** no auto-migration from YAML; documented as intentional.
- **Decider seam change (Phase 3):** touches Approval, Agent, and every Tool definition together; implemented as one slice per the ADR, not incrementally.
- **Selection persistence (Phase 2):** the per-Session override must survive `--resume`, so it is recorded in the Session, not only in memory.
- **Project Trust (Phase 1):** the safety-critical gate; must be exercised at the seam and never default to trusting an untrusted repository.
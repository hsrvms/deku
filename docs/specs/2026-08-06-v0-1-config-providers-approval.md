# Near-term: configuration, provider selection, and approval transparency

**Status:** Implemented; released as `v0.1.0`

## Problem Statement

Deku currently binds one OpenAI-compatible Provider to a single Model at startup through one YAML file plus a handful of environment variables, and its Approval prompts name a Tool's category without showing what the Tool will actually do. A developer who uses several model endpoints — subscriptions and custom URL+key Providers such as tokenrouter, openrouter, and qwencloud — cannot configure or switch between them, and a gated command or Edit is approved blind.

## Solution

Replace the single YAML config with JSON configuration across one **Deku Home** directory and optional **Project Config**, resolved by a defined **Config Precedence** with **Env Substitution**, and gated by **Project Trust** at the project scope. Introduce a **Provider Registry** of named **Providers**, each declaring an **Adapter** family, an optional base URL, its **Authentication**, and the Models it exposes, with runtime **Selection** through a default and a `/model` command. Make Approval transparent: show a **Command Report** before executing and surface Tool output to the terminal. Establish an Agent-to-display activity seam so a future terminal UI can render a **Working Indicator** and **Turn Diff**.

## User Stories

### Configuration

1. As a developer, I want Deku configured by JSON files under one Deku Home directory, so that configuration is standardized and consistent regardless of platform.
2. As a developer, I want configuration split into settings (behavior and Selection), auth (credentials), and models (the Provider Registry's non-secret declaration), so that credentials are isolated from shared configuration.
3. As a developer, I want to reference the environment from configuration with `${VAR}` and provide a fallback with `${VAR:-default}`, so that secrets and machine-specific values stay out of files.
4. As a developer, I want a literal configuration value to override an environment placeholder, so that a higher-precedence source can pin a value the lower source left to the environment.
5. As a developer, I want configuration resolved in the order built-in defaults, then Deku Home global, then Project Config, with environment as a source of values, so that behavior is predictable.
6. As a developer, I want each configuration section replaced as a whole by the next higher-precedence source, so that overrides are predictable rather than merged field-by-field.
7. As a developer, I want a Repository's Project Config loaded only after I grant that project Trust, so that an untrusted repository cannot change my Approval policy.
8. As a developer, I want an explicit startup error when a required value is missing and an environment placeholder is unset with no default, so that misconfiguration fails fast.
9. As a developer, I want a Deku Home `.env` file auto-loaded for secrets and endpoints, so that I need not put secrets in my shell profile.
10. As a developer, I want real process environment variables to win over `.env` and the configuration files, so that CI can override without editing files.

### Provider selection

11. As a developer, I want to define multiple named Providers, each with its own Adapter family, base URL, Authentication, and Model Registry, so that I can use tokenrouter, openrouter, qwencloud, and others from one codebase.
12. As a developer, I want a custom Provider (base URL plus static API key) to use the OpenAI-compatible Adapter with no new wire format, so that URL+key endpoints work out of the box.
13. As a developer, I want each Provider's Authentication stored separately from its Model Registry, so that secrets never travel with shared configuration.
14. As a developer, I want a default Provider and Model for the session via `defaultProvider` and `defaultModel`.
15. As a developer, I want to switch Provider and Model at runtime with a `/model` command, so that I can change models without restarting.
16. As a developer, I want a `/model` change to apply to subsequent Turns and to be restored when the Session resumes, so that my choice persists.
17. As a developer, I want `/model` to offer only Providers the Agent can authenticate to use, so that I choose from what works.
18. As a developer, I want an explicit error when no Provider or Model is selected, so that I know to configure Selection.

### Approval transparency

19. As a developer, I want the Approval prompt to show a Command Report of what a gated Tool Call will do — the exact command, the specific Edit, or the Write to a named path — so that I approve an action, not a Tool name.
20. As a developer, I want Tool execution output echoed to the terminal regardless of the Tool's tier, so that I see what ran on my machine.
21. As a developer, I want Read Tools to continue auto-approving while still showing a Command Report, so that routine inspection stays efficient without losing visibility.
22. As a developer, I want a rejected Tool Call reported and recorded in the Session without executing, so that denial is explicit.

### Activity seam

23. As a developer, I want the Agent to emit a Working Indicator (thinking, working, or awaiting Approval) during a Turn, so that a silent Model call is not mistaken for a hang.
24. As a developer, I want the Agent to emit change events as Edits and Writes happen, so that a future UI can render a live Turn Diff.
25. As a developer, I want the activity stream emitted by the Agent rather than the CLI, so that any renderer consumes one authoritative source.

## Implementation Decisions

- The **config** module is reworked so `Load` returns a `Config` assembled from built-in defaults, Deku Home global sources, and Project Config in Config Precedence order, applying Env Substitution to every value. A **Project Trust** resolver gates whether Project Config is read at all.
- Project Config uses the same three optional modules as Deku Home (`settings.json`, `auth.json`, `models.json`) under a `.deku/` directory at the repository top level, located through the Git top level. The Trust record is `~/.deku/trusted_projects.json`, a list of repository roots; the decision is a deterministic exact-path match, an absent record trusts nothing, and a malformed record fails fast. An untrusted project's files are never read. When a repository carries Project Config and the user runs Deku interactively (terminal input), the CLI asks whether to trust the project; a yes answer records the root in the trust record automatically via `config.GrantTrust` and reloads configuration so the Project Config applies. Non-interactive runs never prompt and never trust. The CLI reports the project scope at startup — loaded, or found but ignored because the project is untrusted — so a user can always tell whether Project Config is in effect.
- The **provider** module is split in two: the **Adapter** (the wire-format translator, whose `Chat` interface is unchanged) and a **ProviderRegistry** that, given a Provider entry, builds the correct Adapter instance through a factory. Authentication is read from the auth source by Provider name.
- **Selection** is a value pairing a Provider and a Model. It has a default (from configuration) and a per-Session override. The Agent accepts a Selection change between Turns and uses the current Selection for the next Turn.
- The **approval** module's `Decider` seam changes so a decision request carries the Tool name, its effective tier, and the rendered **Command Report**. The Gate displays the Report in the prompt; a Tool Call whose Command Report cannot be rendered is refused rather than approved blindly.
- Each built-in **tool** produces the Command Report text for its calls — the exact command, the Edit changes, or the Write path — since only the Tool knows its concrete action.
- The **agent** renders the Command Report at the Approval point, echoes Tool output to the display, and exposes an **activity sink** interface that receives indicator transitions and change events for a future renderer.
- The **CLI** dispatches lines beginning with `/` as commands before they become Turns; `/model` lists selectable Providers and sets the per-Session Selection override.

## Testing Decisions

- The primary seam is the **Agent module** interface, consistent with existing convention: a scripted Provider and a real temporary Git repository observe a complete Turn — Approval with Command Reports, Selection, activity events, Session recording, and Tool outcome.
- The **config** module is tested at `Load` with controlled Deku Home and environment: table-driven cases for Config Precedence, Env Substitution (including missing-placeholder errors), replace-per-section overrides, and the Project Trust gate. This mirrors the existing A environment-then-file tests.
- The **approval** module is tested at the `Gate` with in-memory reader and writer buffers: Command Report rendering per tier, re-prompt on invalid input, and rejection.
- The **provider** module is tested at the registry factory: each Provider entry yields the correct Adapter, and an entry referencing unknown Authentication or an unsupported Adapter fails explicitly.
- No test asserts internal implementation details; all cases observe externally visible behavior through the seams above.

## Out of Scope

- Native subscription Authentication (OAuth login and refresh) for subscription Providers such as claude and codex, and the Anthropic Messages Adapter they require. These are a separate spec.
- The v1 terminal UI renderer: the Working Indicator and live Turn Diff rendered as panes, the `/model` palette shortcut, keybinding policy, themes, and accessibility. Only the Agent-to-display activity seam is in scope here.
- Automatic migration from the existing YAML configuration; switching to JSON is a breaking change.

## Further Notes

- Governed by ADRs `0007-json-config-hierarchy`, `0008-provider-registry-and-selection`, `0009-approval-transparency`, and `0010-terminal-activity-display` (seam portion).
- Project Trust carries the safety rationale: project-scope config can change Approval policy, so untrusted repositories are never loaded.
- Sequence and phase definitions: see the development plan at `docs/specs/2026-08-06-v0-1-development-plan.md`.
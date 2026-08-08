# Deku configuration reference

This reference documents every configuration option in Deku, its source, and
its default. It is the authoritative guide for customizing Deku; the
[README](../README.md) quick start shows a minimal working setup.

Deku is configured by JSON files under one **Deku Home** directory
(`~/.deku/`), optionally overridden per project by **Project Config**, and by
the process environment and a Deku Home `.env` file. Configuration is split by
risk into three optional modules per scope — a missing module is simply absent:

- `settings.json` — behavior: the default Selection, Approval overrides,
  Repository Map exclusions, Agent Commits.
- `auth.json` — credentials: named **Authentication** entries, kept apart from
  the Provider declaration so secrets never travel with shared configuration.
- `models.json` — the non-secret **Provider Registry** declaration.

## Config Precedence

Configuration sources combine in a defined order, from lowest to highest
precedence:

```
built-in defaults  <  Deku Home modules  <  Project Config  <  environment-as-source
```

- **Built-in defaults** apply when no configuration supplies a value.
- **Deku Home modules** are the global configuration under `~/.deku/`.
- **Project Config** is a repository's own modules under `<repo>/.deku/`,
  loaded only **after you grant the project Trust** (see below).
- The **environment-as-source** layer is realized through Env Substitution and
  the Deku Home `.env` file; the real process environment always wins over the
  `.env` file.

Each module is a **section replaced as a whole** by the next higher-precedence
scope that carries it — a trusted project's `settings.json` replaces the Deku
Home `settings.json` entirely, it does not merge field-by-field. A field a
higher-precedence module omits falls back to the built-in default, not to the
lower-precedence Deku Home value. The three modules (`settings`, `auth`,
`models`) are independent sections; a project may replace only one of them.

## Env Substitution

Any string value in a module file may reference the environment:

- `${VAR}` — replaced with the value of the environment variable `VAR`,
  resolved first from the process environment, then from the Deku Home `.env`
  file. An unset variable with no default is an explicit configuration error
  at startup naming the variable and the field, so misconfiguration fails
  fast instead of silently falling back to a default or surfacing later as
  an unrelated error.
- `${VAR:-default}` — uses `default` when `VAR` is unset or empty.

A literal value always wins over a placeholder. The Deku Home `.env` file
(`~/.deku/.env`) holds `NAME=VALUE` lines (blank lines and `#` comments are
ignored) and is the natural place for secrets and machine-specific values; the
real process environment always overrides it.

An **Authentication whose API key does not resolve** (for example `${VAR}`
where `VAR` is unset) is the one deliberate exception to the fail-fast rule:
it leaves its Provider declared but **unable to authenticate**, is excluded
from Selection, and Deku reports explicitly if the selected Provider cannot
authenticate. A missing secret for one Provider never blocks the others.

## `settings.json`

Behavior, including the default Selection. All fields are optional.

| Field | Type | Default | Meaning |
|---|---|---|---|
| `defaultProvider` | string | *(none)* | The default Provider for the session. Must name a declared Provider. |
| `defaultModel` | string | *(none)* | The default Model for the session. Must be one of the default Provider's Models. |
| `approval.tools` | object | *(none)* | Per-tool tier overrides: `"edit": "destructive"`. Value is a tier. |
| `approval.defaults` | object | *(none)* | Per-tier enforcement overrides: `"read": "prompt"`, `"write": "auto"`. Value is an action. |
| `repo_map.exclude` | array of string | `[]` | Gitignore-style glob patterns excluded from the Repository Map. |
| `agent_commits.mode` | string | `"off"` | Agent Commit policy: `off` \| `ask` \| `on`. |
| `agent_commits.validation` | string | `"go test ./..."` | Command run after a completed Turn before an Agent Commit. |

### Approval tiers and actions

Tiers classify a tool's side effects:

| Tier | Meaning |
|---|---|
| `read` | No mutation. Runs un-prompted by default. |
| `write` | Mutates the repository. Prompts by default. |
| `destructive` | Potentially destructive. Prompts with a warning by default. |

Built-in tool classifications:

| Tool | Tier |
|---|---|
| `ls`, `read`, `grep` | `read` |
| `edit`, `write` | `write` |
| `command` | `destructive` |

Actions control enforcement:

| Action | Meaning |
|---|---|
| `auto` | Run without prompting. |
| `prompt` | Pause and ask the user to approve or reject. |

Built-in per-tier defaults:

| Tier | Default action |
|---|---|
| `read` | `auto` |
| `write` | `prompt` |
| `destructive` | `prompt` |

## `models.json`

The **Provider Registry**: a map from Provider name to its declaration. Each
Provider declares an **Adapter family**, a **base URL**, its **Authentication**
by name, and the **Models** it exposes.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `<name>` (key) | object | yes | The Provider name used in Selection and `/model`. |
| `.adapter` | string | yes | The Adapter family. v0.1 supports `openai-compatible`. An unsupported family fails explicitly. |
| `.base_url` | string | yes (openai-compatible) | The API root, e.g. `https://api.openai.com/v1`. |
| `.auth` | string | yes | The name of an Authentication entry in `auth.json`. An unknown name fails explicitly. |
| `.models` | array of string | yes | The Model Registry: the Models this Provider exposes. |

## `auth.json`

A map from **Authentication** name to the typed credential. v0.1 supports only
the `api_key` type; OAuth is a separate specification.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `<name>` (key) | object | yes | The Authentication name referenced by Provider declarations. |
| `.type` | string | yes | The credential type. v0.1 supports `api_key`. An unsupported type fails explicitly. |
| `.api_key` | string | yes | The static API key. Usually an Env Substitution placeholder such as `${OPENAI_API_KEY}`. |

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `DEKU_AGENT_COMMITS` | *(none)* | Environment-as-source override for `agent_commits.mode` (`off` \| `ask` \| `on`). |
| Deku Home `.env` file | *(none)* | Auto-loaded `NAME=VALUE` source for Env Substitution. The real process environment wins over it. |

There is no `DEKU_PROVIDER_*` layer: Provider configuration lives only in the
module files, with secrets supplied through Env Substitution or the `.env` file.

## Project Config and Project Trust

A Repository may carry project-scope configuration in the same three modules
under a `.deku/` directory at the repository top level:

- `.deku/settings.json`
- `.deku/auth.json`
- `.deku/models.json`

Project Config is loaded **only after you grant the project Trust**. When you
run Deku interactively in a repository that carries Project Config, Deku asks
whether to trust the project; a `yes` answer records the repository root in
`~/.deku/trusted_projects.json` and reloads configuration. Non-interactive runs
never prompt and never trust. You can also grant Trust ahead of time by listing
the repository's absolute path:

```json
{ "projects": ["/path/to/repository"] }
```

An **untrusted repository is ignored entirely**: its configuration files are
never read, so they cannot change your Approval policy or other settings. The
Trust decision is deterministic — a repository is trusted only when its
absolute, cleaned path matches a listed path exactly; an absent or empty trust
record trusts nothing. A malformed trust record fails fast.

Because a trusted project's module replaces the Deku Home module of the same
name as a whole, a project `settings.json` that omits `defaultProvider` /
`defaultModel` clears the global defaults. Include them in the project settings
if the project must run with its own Selection.

## Selection and `/model`

The active Provider and Model are a **Selection**. It is resolved from the
per-Session override recorded in the Session transcript, or — when there is no
override — from `defaultProvider` and `defaultModel` in `settings.json`. If no
Selection can be resolved, Deku refuses to start with an explicit error.

During a chat:

```
deku> /model
current selection: openai / gpt-4
openai: gpt-4, gpt-4o
deku> /model openai gpt-4o
selection: openai / gpt-4o
```

- `/model` with no arguments lists the current Selection and every Provider the
  Agent can authenticate to, with its Models.
- `/model <provider> <model>` switches the active Selection for subsequent
  Turns and records the override in the Session, so it is restored when the
  Session resumes (`--resume <session-id>`).
- `/model` lists only Providers the Agent can authenticate to; switching to a
  Provider whose key is missing fails with an explicit error.

## Full defaulted example

A complete configuration that spells out every option and its built-in default.
Values marked "(default)" are the built-in defaults and can be omitted; the
others are required for a working setup.

### `~/.deku/settings.json`

```json
{
  "defaultProvider": "openai",
  "defaultModel": "gpt-4",

  "approval": {
    "tools": {},
    "defaults": {
      "read": "auto",
      "write": "prompt",
      "destructive": "prompt"
    }
  },

  "repo_map": {
    "exclude": []
  },

  "agent_commits": {
    "mode": "off",
    "validation": "go test ./..."
  }
}
```

### `~/.deku/models.json`

```json
{
  "providers": {
    "openai": {
      "adapter": "openai-compatible",
      "base_url": "https://api.openai.com/v1",
      "auth": "openai",
      "models": ["gpt-4", "gpt-4o"]
    }
  }
}
```

### `~/.deku/auth.json`

```json
{
  "openai": {
    "type": "api_key",
    "api_key": "${OPENAI_API_KEY}"
  }
}
```

### `~/.deku/.env`

```sh
# Secrets and machine-specific values. The real process environment wins.
OPENAI_API_KEY=your-api-key
```

With the above, Deku starts with `defaultProvider=openai`, `defaultModel=gpt-4`,
resolving `OPENAI_API_KEY` from the process environment or the `.env` file.
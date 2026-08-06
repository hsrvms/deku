# Provider registry and runtime Model selection

**Status:** accepted

Deku grows from one OpenAI-compatible Provider bound to a single Model at startup into a Provider Registry with runtime Selection. The former single "Provider" concept splits in two: an **Adapter** (the wire-format translator, still the `Chat(ctx, model, system, messages, tools)` interface) and a **Provider** (a named, configured account that declares an Adapter family, an optional base URL, its Authentication, and the Models it exposes). Authentication is typed — `api_key` or `oauth` — so a subscription provider (claude, codex) is simply a Provider whose Authentication is OAuth, while a custom provider (tokenrouter, openrouter, qwencloud) is a Provider authenticated by a static API key. Selection resolves which Provider and Model the Agent uses, from a `defaultProvider`/`defaultModel` default and a per-Session `/model` override.

## Considered options

- Keep one global Provider and single Model. Rejected because it cannot express multiple subscriptions or custom URL+key endpoints.
- Model a separate "subscription" entity distinct from custom providers. Rejected because both are Providers that differ only in Authentication type and Adapter family.
- Support only custom (URL+key) providers first and defer subscriptions. Adopted as the implementation order: custom providers reuse the existing OpenAI-compatible Adapter and need no new wire format, whereas native subscriptions (Claude, Codex) additionally require an Anthropic Messages Adapter and an OAuth login/refresh flow, which is its own feature.

## Consequences

The Agent's `Chat` seam does not change; Selection only requires constructing the right Adapter instance from a Provider entry and letting the Agent swap the active Provider+Model between Turns. The `/model` command sets a per-Session override that is restored on resume; the global default applies otherwise. Native subscription support is scoped separately because it adds a second Adapter family and an OAuth lifecycle.
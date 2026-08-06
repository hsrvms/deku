# JSON configuration with environment substitution and a scoped hierarchy

**Status:** accepted

Deku replaces its single YAML config file (`~/.deku/config.yaml`) plus a few environment variables with standardized JSON configuration spread across modular files under one Deku Home directory, and defines a precedence chain with environment substitution. Configuration sources combine as `defaults < global (Deku Home) < project (Project Config) < environment-as-source`: each section is replaced as a whole by the next higher-precedence source, and the environment is a *source* of values — via explicit `${VAR}` / `${VAR:-default}` substitution — rather than a separate precedence layer. Configuration is split by risk into `settings.json` (behavior and Selection), `auth.json` (credentials), and `models.json` (the Provider Registry's non-secret declaration). Project Config is loaded only after the user grants Project Trust.

## Considered options

- Keep YAML. Rejected because a standardized, machine-friendly format reduces dialect drift and tool friction; JSONC was considered and rejected in favor of plain JSON so the format is fully standardized.
- Split configuration and data across `~/.config` and platform data directories (XDG). Rejected in favor of one Deku Home directory for simplicity and a stricter compatibility story.
- Treat the environment as a soft fallback layer over the file. Rejected in favor of explicit substitution so the config file is self-documenting about its inputs and any value can be environment-driven.
- Merge configuration field-by-field. Rejected in favor of replace-per-section for predictable behavior.

## Consequences

Configuration moves from one file to three optional modules per scope; a missing module is simply absent. Credentials live in `auth.json`, kept separate and tightly permissioned, and never travel with the Provider Registry. Because the format and layout are user-facing and change where settings live, this is a breaking change for existing users. The format is resolved before the Provider Registry builds on it.
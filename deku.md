
# Project Name

**Deku**

*A minimal, extensible coding agent platform written in Go.*

> **Origin:** *Deku* (木偶) historically referred to a wooden puppet or figure that performs work on behalf of another. For this project, the name is intentionally interpreted as a humble worker rather than an expert. The emphasis is on being a dependable tool that carries out tasks under the user's direction—not an autonomous engineer.

---

# Vision

Deku is a terminal-first, AI-powered software engineering platform that combines:

* Pi's minimal agent architecture
* Aider's repository intelligence and Git workflow
* Go's simplicity, concurrency, and portability
* A first-class extension ecosystem
* Self-improving capabilities through agent-authored extensions

Deku should be usable as a daily coding assistant while remaining understandable enough that one developer can comprehend the entire architecture.

The core philosophy is:

> The framework should remain small. Intelligence belongs in the model. Capabilities belong in extensions.

---

# CLI

Examples

```text
deku

deku chat

deku review

deku explain

deku commit

deku index

deku extensions

deku provider

deku doctor

deku session

deku memory

deku repo
```

---

# Extension Layout

```text
~/.deku/

config.yaml

extensions/

sessions/

memory/

cache/
```

Example extension:

```text
~/.deku/extensions/docker-review/

manifest.yaml

SYSTEM.md

tools/

tests/

README.md
```

---

# Repository Structure

```text
cmd/

    deku/

internal/

    agent/

    session/

    prompt/

    approval/

    repository/

    planner/

    capability/

    extension/

    provider/

    git/

    patch/

pkg/

    sdk/

    mcp/

    tui/

    config/
```

---

# Commands

```bash
deku chat
deku review
deku explain
deku commit
deku repo summarize
deku repo graph
deku extension new docker-review
deku extension install github
deku provider list
deku doctor
```

---

# Configuration

```yaml
provider: anthropic

model: claude-sonnet

approval:
  destructive: ask

repository:
  indexing: true

extensions:
  - git
  - repo
  - docker

memory:
  repository: true
```

Default location:

```text
~/.deku/config.yaml
```

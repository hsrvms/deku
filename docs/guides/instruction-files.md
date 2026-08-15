# System prompt instruction files

Deku's System Prompt is assembled for every Step from ordered layers: the
Base System Prompt (Deku's built-in instructions), the **Global
Instructions**, the **Project Instructions**, and finally the per-Step
machinery — the Repository Map with its instruction and truncation note.

Three plain markdown files let you add to that prompt, or replace the
built-in voice entirely:

| File | Location | What it does |
| --- | --- | --- |
| Global Instructions | `~/.deku/AGENTS.md` | Your always-on instructions, applied in every project. |
| System Prompt Override | `~/.deku/SYSTEM.md` | Replaces Deku's built-in Base System Prompt wholesale. |
| Project Instructions | `<repository>/AGENTS.md` | The repository's always-on instructions, applied to every Turn in that repository. |

## How the layers compose

The System Prompt is assembled in a fixed order:

1. **The Base System Prompt** — or the System Prompt Override when
   `~/.deku/SYSTEM.md` exists.
2. **Global Instructions** — `~/.deku/AGENTS.md`.
3. **Project Instructions** — the repository's root `AGENTS.md`.
4. **The machinery** — the Repository Map with its instruction and
   truncation note (and the Skill catalog when Skills land).

Specificity increases downward: global preferences first, then the
repository's conventions, then the per-Step map. Instruction layers are
additive — Deku does not arbitrate conflicts between them; the model weighs
them. An override replaces **only** the Base System Prompt: your Global
Instructions, the repository's Project Instructions, and the machinery all
survive it. The machinery layer is never replaced, because it is per-Step
input rather than voice.

## Semantics

- **A missing file is simply absent.** Deku runs unchanged with none of the
  three files present.
- **A file that exists but cannot be read fails startup** with an explicit
  error naming the file. A broken instruction file is never silently
  ignored.
- **No token budget, no truncation.** Instruction files are never truncated;
  you control their size. Keep them lean — every byte is sent with every
  Step of every Turn.
- **Stable for the whole Session.** Instruction files are loaded once at
  Session start and the same set applies to every Step of every Turn; the
  per-Step System Prompt differs only in the freshly built Repository Map.
- **Project Instructions load regardless of Project Trust.** A root
  `AGENTS.md` is repository content, not policy — the same class of input as
  any file the Agent reads during a Turn. Trust gates only what lives under
  the repository's `.deku/` directory (Project Config, project Skills).
- **Instructions cannot weaken safety.** They steer the model; they cannot
  change Approval policy, tool availability, or any other safety behavior,
  which are enforced by machinery, not by the prompt.

## Writing good instructions

### Global Instructions (`~/.deku/AGENTS.md`)

How you want to be worked with, across every project. Typical content:

- response style and format preferences (concise answers, explain trade-offs,
  no filler);
- habits you want the agent to always follow (run the tests before claiming
  success, never touch generated files, ask before deleting anything);
- tools and workflows you prefer.

### Project Instructions (`<repository>/AGENTS.md`)

How this repository wants to be worked on, for every agent that reads it.
`AGENTS.md` is the emerging cross-agent convention, so a repository that
already carries instructions for other agents works with Deku unchanged.
Typical content:

- build, test, and validation commands and the order to run them;
- architecture notes: which modules exist, what each owns, what to read
  first;
- conventions to preserve and pitfalls to avoid;
- anything the repository wants every agent Turn to respect.

### System Prompt Override (`~/.deku/SYSTEM.md`)

Your own voice, replacing Deku's built-in base prompt entirely. Write the
full persona and working rules you want — nothing of the Base System Prompt
survives. The Global Instructions, Project Instructions, and machinery still
attach below it, so you do not need to restate those.

## Example

`~/.deku/AGENTS.md`:

```markdown
# Global instructions

- Answer in short paragraphs. State assumptions explicitly.
- After any code change, run the project's formatter and tests.
- Never edit generated or vendored files.
```

`<repository>/AGENTS.md`:

```markdown
# Project instructions

This repository is a Go module. Before finishing a change:

1. Run `gofmt -l .` and fix any files it lists.
2. Run `go vet ./...`.
3. Run `go test ./...`.

The `prompt` module owns system prompt assembly; the `agent` module owns the
Turn loop. Do not move orchestration between them.
```

`~/.deku/SYSTEM.md`:

```markdown
You are a senior software engineer working in the user's terminal.

Work directly from the user's request. Inspect the repository before making
claims about it, and report tool failures honestly. When you propose a
change, explain the trade-offs and risks.
```

With all three files present, every Step's System Prompt is: the override
above, then the global instructions, then the project instructions, then the
Repository Map.

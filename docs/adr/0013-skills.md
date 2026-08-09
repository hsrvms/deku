# Skills: model-selected instruction files

**Status:** accepted

Deku gains **Skills** as a distinct kind of prompt content: a named markdown instruction file (name, description, body) that teaches the Agent how to perform a recurring task. Skills live in `~/.deku/skills/<name>/` or a trusted project's `.deku/skills/`, carry **instructions only — never Tools** — and are loaded by **model-side selection**: the prompt carries a compact catalog of Skill names and descriptions, and the Agent reads a Skill's body when the current request matches its description. Users may also invoke a Skill explicitly with the `/skill:<name>` Command, which lives in its own namespace so user-authored Skill names can never collide with product Commands (a user Skill named `review` cannot shadow the Purpose Command).

The three kinds of prompt content are now explicitly bounded:

- **Extension `SYSTEM.md`** — always-on fragment, loaded every Turn while the extension is enabled.
- **Skill** — conditional instructions, loaded by the Agent when relevant.
- **Purpose Command** — user-invoked experience with a fixed purpose prompt and a purpose-scoped Tool set.

Tools always come from Tools; the boundary keeps each concept cheap.

## Considered options

- **Framework-side skill matching** (heuristic scoring of request against descriptions). Rejected: it duplicates the Repository Map's ranking machinery and contradicts "intelligence belongs in the model" (ADR-0001). The catalog-plus-read approach needs no new runtime machinery — skills are markdown files readable with the existing `read` Tool.
- **Skills callable as flat `/name` Commands.** Rejected: user-authored names would collide with the product Command namespace; `/skill:<name>` keeps the namespaces separate.
- **Skills that carry Tools.** Rejected: Tools already have a home (built-in and Extension Tools); giving Skills Tools would blur the Skill/Purpose Command boundary and make Skills a second extension mechanism.

## Consequences

Skills join the v1 scope (roadmap). The glossary defines Command, Purpose Command, and Skill with explicit disambiguation (Command vs. Command Report; the `commit` Purpose Command vs. Agent Commit). Resolved format decisions: the Skill file is `SKILL.md` with a JSON front matter block (name, description), consistent with the JSON standardization of configuration (ADR-0007) and extension manifests; a project Skill of the same name replaces the global Skill, consistent with Config Precedence's replace-per-scope rule, and duplicate names within one scope fail fast at discovery naming the file; the Skill catalog is name-plus-description, bounded by a token budget, and truncated with a note when it exceeds it, following the Repository Map pattern. Remaining spec-level detail: discovery timing, malformed-Skill handling, and any interaction between Purpose Commands and Skills.

# User and project instruction layers in the system prompt

Deku's system prompt was a single built-in string plus the Repository Map, and ADR-0013 bounded prompt content into three kinds (Extension SYSTEM.md, Skill, Purpose Command). Before implementing Skills, Deku decided how users add to or replace the system prompt and how repositories express always-on instructions. The system prompt is now an ordered assembly of layers: the Base System Prompt (replaceable by a System Prompt Override), the Global Instructions, the Project Instructions, enabled Extensions' SYSTEM.md fragments, the Turn's purpose prompt or explicitly invoked Skill body when present, and finally the per-Step machinery (Repository Map, Skill catalog, truncation notes). Instruction layers are additive — Deku never arbitrates conflicts between them — and the machinery layer is never replaced: it is per-Step input, not voice.

**Status:** accepted

## Decisions

- **Names and locations.** Project Instructions are `AGENTS.md` at the Repository root; Global Instructions are `AGENTS.md` in the Deku Home; the System Prompt Override is `SYSTEM.md` in the Deku Home. `AGENTS.md` is the emerging cross-agent convention, so repositories that already carry instructions for other agents work with Deku unchanged.
- **Override replaces only the Base System Prompt.** An override removes the product's voice; user and project instructions, extension fragments, and machinery all survive. The override cannot weaken Approval or tool availability, which are enforced by machinery, not by the prompt.
- **Trust follows a directory boundary.** Everything under a Repository's `.deku/` directory (Project Config, project Skills) requires Project Trust; a root `AGENTS.md` loads regardless of trust. A root AGENTS.md is repository content — the same class of input as any file the Agent reads during a Turn — and a hostile one is no stronger than a hostile README: every mutation still gates on Approval.
- **Named Skills replace per scope; instruction files layer.** ADR-0013's replace rule and this ADR's additive rule coexist because the content kinds differ: a same-named task procedure is one thing, and the project's version wins; general guidance is complementary. Specificity increases downward: base, global, project, Turn.
- **No token budgets for always-on instruction files.** Their authors control their size; the Skill catalog keeps its budget because it is machinery.

## Considered options

- **`DEKU.md` or `.deku/AGENTS.md` for project instructions.** Rejected: no cross-agent portability, and a `.deku/` location would drag instructions behind Trust.
- **Instructions as `settings.json` fields.** Rejected: markdown in JSON is hostile to authors and diffs, and replace-per-section would let a project's fields override the user's instructions.
- **Trust-gated `AGENTS.md`.** Rejected: a false security boundary, and inconsistent with the Agent's treatment of other repository content.
- **Project-scope override (a `SYSTEM.md` or `AGENTS.override.md` inside a Repository).** Deferred: not a stated need; can be added later without breaking the layer model.
- **A configuration toggle to disable instruction file loading.** Deferred: a non-breaking config addition later.

## Consequences

The prompt-content taxonomy of ADR-0013 grows from three kinds to five — always-on (Extension SYSTEM.md, Global Instructions, Project Instructions), conditional (Skill), user-invoked (Purpose Command) — plus the non-negotiable machinery layer. The glossary gains System Prompt, Base System Prompt, Global Instructions, Project Instructions, and System Prompt Override, with Project Config and Project Trust sharpened around the content/policy and directory boundaries. The v1 specification gains user stories and implementation decisions for system prompt customization, folded into the Skills workstream. ADR-0013's open item "interaction between Purpose Commands and Skills" resolves: read-only Purpose Command tool sets include `read`, so model-selected Skills keep working inside them, and `/skill:<name>` is always its own Turn. Issues #56 and #63 are unaffected — their checklists are consistent with this ADR: project Skills remain trust-gated, the Skill catalog is machinery and survives an override, and explicit invocation is Turn-scoped content.

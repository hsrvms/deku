# Extension tool kinds: External Tools and MCP Tools

**Status:** accepted

**Supersedes:** [0002-mcp-extension-tools.md](0002-mcp-extension-tools.md)

Extensions are Deku's packaging unit for model-visible capabilities beyond the built-in Tools: a directory under `~/.deku/extensions/<name>/` containing a JSON manifest and a `SYSTEM.md` prompt fragment, discovered by scanning the filesystem and enabled by listing the extension in the Deku Home settings module. The manifest is JSON, not YAML, for the same reason ADR-0007 standardized configuration on plain JSON: one standardized, machine-friendly format across Deku's user-editable files.

Extension Tools come in two kinds, both behind the single Tool seam:

- **External Tool** — execution is a command declared in the manifest: any executable or script, in any language. Each call spawns a fresh process; the call's arguments are passed as JSON and the command's stdout becomes the Tool Result (structured when it parses as JSON, text otherwise; stderr is captured as error detail). An External Tool has no state between calls, so a crash is an ordinary failed Tool Result and there is no process lifecycle to manage. The manifest declares each Tool's Approval tier; an undeclared tier defaults to Write, so unvetted extension code prompts before every execution.
- **MCP Tool** — execution is bridged to a Tool exposed by an MCP server over stdio, as ADR-0002 originally scoped. The server supplies Tool definitions at runtime; the manifest declares the server command and may restrict which of the server's Tools are bridged (an allowlist, so a server that grows an unapproved Tool stays inert) and declare tiers, defaulting to Write.

## Considered options

- **MCP stdio only** (ADR-0002 as the entire story). Rejected: the protocol ceremony makes a simple "drop a script" extension need a JSON-RPC server, the opposite of the pi-style simplicity Deku wants. A subprocess is unavoidable in Go — there is no practical in-process loading of user code for distributed binaries — but the contract itself can be nearly free.
- **External Tools only.** Rejected: it cuts Deku off from the existing MCP server ecosystem and from stateful, long-lived tools that per-call spawning cannot express.
- **In-process scripting VM (Lua, Starlark, WASM) or Go plugins.** Rejected: a scripting runtime is a heavy dependency against the standard-library-first rule, and Go's plugin mechanism requires build-identical binaries, which cannot work for distributed releases.

## Consequences

Two dispatchers — command spawn and MCP bridge — sit behind one Tool interface; Approval, Command Report, and Tool Output machinery apply unchanged to both kinds. External Tools are the primary authoring path and are agent-authorable, the prerequisite for the later-horizon Agent-authored Extensions. MCP bridging is the ecosystem and stateful path. The v1 specification sequences External Tools first and MCP bridging second. Linux is the target platform; External Tool commands are declared in the platform's own style.

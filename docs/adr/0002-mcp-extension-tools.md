# Use MCP stdio servers for extension tools

**Status:** superseded by [0011-extension-tool-kinds.md](0011-extension-tool-kinds.md). MCP stdio remains one kind of Extension Tool; the simple authoring path is the External Tool, a command declared in the extension's manifest.

Deku needs runtime-loadable extension tools without coupling them to the host binary's Go build. Extension tools run as MCP servers over stdio while built-in tools remain in-process, trading a heavier protocol for process isolation, language independence, and compatibility with existing MCP servers.

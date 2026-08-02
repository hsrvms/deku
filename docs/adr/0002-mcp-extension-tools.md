# Use MCP stdio servers for extension tools

Deku needs runtime-loadable extension tools without coupling them to the host binary's Go build. Extension tools run as MCP servers over stdio while built-in tools remain in-process, trading a heavier protocol for process isolation, language independence, and compatibility with existing MCP servers.

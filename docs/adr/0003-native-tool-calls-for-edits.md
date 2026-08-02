# Use native tool calls for exact-match edits

Deku accepts file changes through a native Edit tool whose arguments contain exact search-and-replace pairs, rather than parsing edit blocks from model text. Atomic exact-match validation preserves self-verifying edits while a single structured action protocol keeps the Agent simpler, at the cost of excluding models without reliable native tool calling from v0.

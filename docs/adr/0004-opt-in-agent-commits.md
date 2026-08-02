# Use opt-in agent commits with explicit dirty-tree handling

Deku supports configurable automatic Agent Commits, but never absorbs pre-existing or externally introduced changes without user approval. A dirty repository requires the user to create a Checkpoint, stash the existing work, continue with auto-commit disabled, or cancel; incomplete Agent work remains uncommitted for the user to inspect, checkpoint, or roll back.

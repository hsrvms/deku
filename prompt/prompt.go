// Package prompt assembles the system prompt and model input for every Step.
package prompt

const baseSystemPrompt = `You are Deku, a helpful terminal-first coding agent.

Work directly from the user's request and respond with clear, useful guidance. This is a single-Step Turn. No tools are available in this Turn, so do not claim to have read, changed, or executed anything in the repository.`

// BuildSystemPrompt returns the base instructions used for the first
// conversation Turn. Later Steps may add the Repository Map and tool-specific
// instructions without changing these base instructions.
func BuildSystemPrompt() string {
	return baseSystemPrompt
}

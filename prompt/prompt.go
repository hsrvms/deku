// Package prompt assembles the system prompt and model input for every Step.
package prompt

import "strings"

const baseSystemPrompt = `You are Deku, a helpful terminal-first coding agent.

Work directly from the user's request and respond with clear, useful guidance. A Turn may contain multiple Steps. Use the available read-only tools to inspect the repository before making claims about its contents. Tool results are authoritative; report tool failures and do not claim to have changed files.`

// mapInstruction is attached whenever a Repository Map is injected.
const mapInstruction = `The map below shows the repository's file structure. The map shows file paths, not source code. Use ` + "`read`" + ` to obtain file contents before editing.`

// BuildSystemPrompt returns the instructions used for every Step. When repoMap
// is non-empty, the Repository Map is injected so the model gains structural
// orientation without mistaking paths for implementations.
func BuildSystemPrompt(repoMap string) string {
	if strings.TrimSpace(repoMap) == "" {
		return baseSystemPrompt
	}
	return baseSystemPrompt + "\n\n## Repository Map\n\n" + mapInstruction + "\n\n" + repoMap
}

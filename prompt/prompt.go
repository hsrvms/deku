// Package prompt assembles the system prompt and model input for every Step.
package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const baseSystemPrompt = `You are Deku, a helpful terminal-first coding agent.

Work directly from the user's request and respond with clear, useful guidance. A Turn may contain multiple Steps. Use the available read-only tools to inspect the repository before making claims about its contents. Tool results are authoritative; report tool failures and do not claim to have changed files.`

// mapInstruction is attached whenever a Repository Map is injected.
const mapInstruction = `The map below shows the repository's file structure. The map shows file paths, not source code. Use ` + "`read`" + ` to obtain file contents before editing.`

// Instruction file names. Global Instructions and the System Prompt Override
// live in the Deku Home; Project Instructions live at the Repository root.
const (
	agentsFile = "AGENTS.md"
	systemFile = "SYSTEM.md"
)

// Instructions is the loaded set of system prompt instruction files: the
// System Prompt Override, the user's Global Instructions, and the
// Repository's Project Instructions. A layer is empty when its file is
// absent or blank. A nil *Instructions behaves identically to an empty one,
// so callers that have no instruction set pass nil and get the plain Base
// System Prompt.
type Instructions struct {
	// Override replaces the Base System Prompt wholesale when non-empty.
	// Every other layer — Global, Project, and the machinery — survives it.
	Override string
	// Global is the user's always-on instructions, from AGENTS.md in the
	// Deku Home.
	Global string
	// Project is the Repository's always-on instructions, from AGENTS.md at
	// the Repository root.
	Project string
}

// LoadInstructions discovers the system prompt instruction files: AGENTS.md
// in the Deku Home (Global Instructions), SYSTEM.md in the Deku Home (System
// Prompt Override), and AGENTS.md at the Repository root (Project
// Instructions). A missing file is simply absent; a file that exists but
// cannot be read is an explicit error naming the file, so a broken
// instruction file is never silently ignored.
//
// The project file is read regardless of trusted: a root AGENTS.md is
// repository content, not policy, and cannot change Approval, tool
// availability, or any safety behavior. Nothing is ever read from a
// Repository's .deku/ directory, because no project-scope instruction files
// exist in v1; the trusted parameter keeps the seam stable for the
// project-scope files ADR-0014 defers. projectRoot is the Repository's
// top-level directory, or "" when the process is not inside a Repository, in
// which case there is no Project layer.
func LoadInstructions(dekuHome, projectRoot string, trusted bool) (*Instructions, error) {
	var instructions Instructions
	var err error
	if instructions.Global, err = readInstructionFile(filepath.Join(dekuHome, agentsFile)); err != nil {
		return nil, err
	}
	if instructions.Override, err = readInstructionFile(filepath.Join(dekuHome, systemFile)); err != nil {
		return nil, err
	}
	if projectRoot != "" {
		if instructions.Project, err = readInstructionFile(filepath.Join(projectRoot, agentsFile)); err != nil {
			return nil, err
		}
	}
	return &instructions, nil
}

// readInstructionFile reads one instruction file. An absent file yields ""
// with no error; a file that exists but cannot be read yields an error
// naming the file; a file whose content is blank (empty or whitespace only)
// yields "", so an empty instruction file contributes no layer.
func readInstructionFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read instruction file %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", nil
	}
	return string(data), nil
}

// BuildSystemPrompt returns the instructions used for every Step, assembled
// from ordered layers: the System Prompt Override when present, else the
// Base System Prompt; the Global Instructions; the Project Instructions;
// then the per-Step machinery — the Repository Map with its instruction and
// truncation note when a map is provided. An override replaces only the
// base layer; instruction layers are additive, carry no token budget, and
// are never truncated. A nil instruction set composes the Base System Prompt
// and the Repository Map exactly as before.
func BuildSystemPrompt(repoMap string, instructions *Instructions) string {
	base := baseSystemPrompt
	if instructions != nil && strings.TrimSpace(instructions.Override) != "" {
		base = instructions.Override
	}
	layers := []string{base}
	if instructions != nil && strings.TrimSpace(instructions.Global) != "" {
		layers = append(layers, instructions.Global)
	}
	if instructions != nil && strings.TrimSpace(instructions.Project) != "" {
		layers = append(layers, instructions.Project)
	}
	system := strings.Join(layers, "\n\n")
	if strings.TrimSpace(repoMap) != "" {
		system += "\n\n## Repository Map\n\n" + mapInstruction + "\n\n" + repoMap
	}
	return system
}

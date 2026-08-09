package tui

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"

	"github.com/hsrvms/deku/repository"
)

// Turn Diff pane limits: a file's diff contributes at most diffFileCap lines
// and the whole pane at most diffTotalCap lines, each cut with a truncation
// note. The caps bound the pane's content; they are not a correctness claim.
const (
	diffFileCap  = 200
	diffTotalCap = 1000
	// diffPaneDivisor: the Turn Diff pane takes one third of the main area's
	// height and the Transcript the rest, per the design guide's layout.
	diffPaneDivisor = 3
)

// diffFunc computes the cumulative per-file working-tree diff of paths: one
// unified diff per path, keyed by path, "" for a path with no working-tree
// changes. It is the Turn Diff pane's test seam; the production default
// computes the diff with git in the working directory's repository.
type diffFunc func(paths []string) (map[string]string, error)

// gitDiff returns the production diffFunc: the cumulative working-tree diff
// of paths, computed with git in the repository containing the working
// directory (discovered once on first use). The Turn Diff is a display of
// Agent work — the renderer reads git, it never stages, stashes, or commits —
// and when no repository exists the pane reports that instead of failing the
// UI.
func gitDiff() diffFunc {
	var once sync.Once
	var repo *repository.Repo
	var err error
	return func(paths []string) (map[string]string, error) {
		once.Do(func() {
			root, rootErr := repository.Root(".")
			if rootErr != nil {
				err = rootErr
				return
			}
			if root == "" {
				err = errors.New("not a Git repository")
				return
			}
			repo, err = repository.New(root)
		})
		if err != nil {
			return nil, err
		}
		return repo.Diff(paths)
	}
}

// diffEntry is one changed file's contribution to the pane: its diff lines
// with the per-file cap already applied.
type diffEntry struct {
	path          string
	lines         []string
	fileTruncated bool
}

// renderTurnDiff lays the Turn Diff pane out at the given width: a header,
// then each changed file's cumulative diff in Change order. A diff failure
// (for example no Git repository) renders as a note; the pane is a display of
// Agent work and never fails the UI. The per-file and total line caps each
// carry a truncation note.
func renderTurnDiff(diffs map[string]string, order []string, width int, diffErr error) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(palette.rule).Render("Turn Diff"))
	b.WriteByte('\n')
	b.WriteString(lipgloss.NewStyle().Foreground(palette.rule).Render(strings.Repeat("─", width)))
	b.WriteByte('\n')
	if diffErr != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(palette.rule).Render("diff unavailable: " + diffErr.Error()))
		return strings.TrimSuffix(lipgloss.NewStyle().Width(width).Render(b.String()), "\n")
	}

	remaining := diffTotalCap
	totalTruncated := false
	entries := diffEntries(diffs, order)
	for i, e := range entries {
		if len(e.lines) > remaining {
			e.lines = e.lines[:remaining]
			totalTruncated = true
		} else {
			remaining -= len(e.lines)
		}
		for _, line := range e.lines {
			b.WriteString(styleDiffLine(line))
			b.WriteByte('\n')
		}
		if e.fileTruncated {
			b.WriteString(truncationNote(fmt.Sprintf("%s diff truncated at %d lines", e.path, diffFileCap)))
			b.WriteByte('\n')
		}
		if remaining == 0 {
			if i < len(entries)-1 {
				totalTruncated = true
			}
			break
		}
	}
	if totalTruncated {
		b.WriteString(truncationNote(fmt.Sprintf("diff truncated at %d lines", diffTotalCap)))
		b.WriteByte('\n')
	}
	return lipgloss.NewStyle().Width(width).Render(strings.TrimSuffix(b.String(), "\n"))
}

// diffEntries collects the changed files' diff lines in Change order, drops
// paths with no working-tree changes, and applies the per-file cap.
func diffEntries(diffs map[string]string, order []string) []diffEntry {
	var entries []diffEntry
	for _, path := range order {
		diff := diffs[path]
		if diff == "" {
			continue
		}
		lines := diffLines(diff)
		fileTruncated := len(lines) > diffFileCap
		if fileTruncated {
			lines = lines[:diffFileCap]
		}
		entries = append(entries, diffEntry{path: path, lines: lines, fileTruncated: fileTruncated})
	}
	return entries
}

// diffLines splits a unified diff into its lines, treating a trailing
// newline as a terminator, not a blank line.
func diffLines(diff string) []string {
	lines := strings.Split(strings.TrimSuffix(diff, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// styleDiffLine colors added and removed content lines with their semantic
// tokens. The + and - prefixes always remain visible, so color is never the
// only signal (design guide §6); header lines (--- a/, +++ b/, @@) stay
// plain.
func styleDiffLine(line string) string {
	switch {
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "++"):
		return lipgloss.NewStyle().Foreground(palette.diffAdd).Render(line)
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "--"):
		return lipgloss.NewStyle().Foreground(palette.diffDel).Render(line)
	default:
		return line
	}
}

// truncationNote renders a muted note that a cap cut the diff.
func truncationNote(text string) string {
	return lipgloss.NewStyle().Foreground(palette.rule).Render("... " + text)
}

// padToHeight pads s to exactly height lines so the Transcript and Turn Diff
// columns join into a full-height frame.
func padToHeight(s string, height int) string {
	lines := strings.Count(s, "\n") + 1
	if lines >= height {
		return s
	}
	return s + strings.Repeat("\n", height-lines)
}

// cutToHeight keeps the first height lines of s: the pane's window onto its
// capped content. The pane is top-anchored and does not scroll (no ratified
// binding exists for it), so a diff taller than the pane shows its beginning.
func cutToHeight(s string, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		return s
	}
	return strings.Join(lines[:height], "\n")
}

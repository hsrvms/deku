// Package repository owns Git inspection, dirty-tree handling, Checkpoints,
// Validation, and Agent Commit logic. It is a concrete deep module backed by
// the real Git command-line tool; v0 has no Repository interface because there
// is exactly one implementation and no demonstrated second adapter.
package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Mode is the Agent Commit configuration. It controls whether Deku creates
// Git commits of Agent-owned changes after a completed Turn.
type Mode string

// Agent Commit modes.
const (
	// ModeOff never creates Agent Commits and never prompts about them.
	ModeOff Mode = "off"
	// ModeAsk prompts the user before creating each Agent Commit.
	ModeAsk Mode = "ask"
	// ModeOn creates an Agent Commit automatically after a validated Turn.
	ModeOn Mode = "on"
)

// Valid reports whether m is a known Agent Commit mode.
func (m Mode) Valid() bool {
	switch m {
	case ModeOff, ModeAsk, ModeOn:
		return true
	default:
		return false
	}
}

// ParseMode converts a configuration value into a Mode. It rejects unknown
// values so configuration errors fail fast at startup.
func ParseMode(value string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(value)))
	if !mode.Valid() {
		return "", fmt.Errorf("unknown agent_commits mode %q (want off, ask, or on)", value)
	}
	return mode, nil
}

// State describes the working tree relative to HEAD.
type State struct {
	Clean     bool
	Staged    []string
	Unstaged  []string
	Untracked []string
}

// Dirty reports whether the working tree holds any uncommitted changes.
func (s State) Dirty() bool {
	return !s.Clean
}

// Validation describes the outcome of running the repository's checks.
type Validation struct {
	Command string
	Passed  bool
	Output  string
}

// Repo performs Git operations on one working tree.
type Repo struct {
	root string
}

// Root returns the absolute top-level directory of the Git repository that
// contains dir, or "" when dir is not inside a Git repository. It locates the
// project scope: Project Config lives in a .deku directory at the repository
// top level, so the top level, not the current directory, is what matters.
//
// Only "not inside a Git repository" yields "", nil — the legitimate absence
// of project scope. Any other failure to inspect the directory (Git
// unavailable, a missing or unreadable directory, a malformed repository)
// returns a contextual error, so a caller never silently runs without
// project scope.
func Root(dir string) (string, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve directory: %w", err)
	}
	cmd := exec.Command("git", "-C", absolute, "rev-parse", "--show-toplevel")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		top := strings.TrimSpace(string(out))
		if top == "" {
			return "", nil
		}
		return filepath.Abs(top)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		// Git itself could not be started (for example it is not installed):
		// a real failure, not an absent repository.
		return "", fmt.Errorf("locate git repository for %q: %w", absolute, err)
	}
	// Only the exact legitimate absence message means "no project scope".
	// Git's malformed-repository error also begins "not a git repository"
	// ("fatal: not a git repository: /path/.git" when a .git gitdir file
	// points nowhere), and that is a real failure the caller must see.
	if strings.Contains(stderr.String(), "not a git repository (or any of the parent directories)") {
		// Not inside a Git repository: there is legitimately no project
		// scope.
		return "", nil
	}
	return "", fmt.Errorf("locate git repository for %q: %w: %s", absolute, err, strings.TrimSpace(stderr.String()))
}

// New validates root and constructs a Repo backed by the Git repository there.
// A directory that is not a Git repository is rejected so callers fail fast
// before a Turn begins.
func New(root string) (*Repo, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("repository root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("stat repository root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository root %q is not a directory", root)
	}
	repo := &Repo{root: absolute}
	if err := repo.checkGitDir(); err != nil {
		return nil, err
	}
	return repo, nil
}

// Root returns the absolute repository root.
func (r *Repo) Root() string {
	if r == nil {
		return ""
	}
	return r.root
}

// checkGitDir verifies that root belongs to a Git repository.
func (r *Repo) checkGitDir() error {
	if _, err := r.git("rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("%q is not a Git repository", r.root)
	}
	return nil
}

// State inspects the working tree and classifies staged, unstaged, and
// untracked paths relative to the index and HEAD.
func (r *Repo) State() (State, error) {
	out, err := r.git("status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return State{}, err
	}
	return parseStatus(out), nil
}

// parseStatus parses `git status --porcelain --untracked-files=all` output
// into a State, treating the first status column as the index state and the
// second as the working-tree state.
func parseStatus(out string) State {
	var st State
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		index := line[0]
		worktree := line[1]
		path := strings.TrimSpace(line[3:])
		// Renames use the form "old -> new"; attribute the new path.
		if arrow := strings.Index(path, " -> "); arrow >= 0 {
			path = path[arrow+4:]
		}
		switch index {
		case '?':
			st.Untracked = append(st.Untracked, path)
			continue
		case ' ', '!':
			// Not staged in the index.
		default:
			st.Staged = append(st.Staged, path)
		}
		if worktree != ' ' {
			st.Unstaged = append(st.Unstaged, path)
		}
	}
	st.Clean = len(st.Staged) == 0 && len(st.Unstaged) == 0 && len(st.Untracked) == 0
	return st
}

// Snapshot records the current content hashes of the working tree so later
// changes can be attributed to the Agent during one Turn.
type Snapshot struct {
	tracked   map[string]string
	untracked map[string]string
}

// Snapshot captures the content of every tracked file and every untracked,
// non-ignored file at this moment. A tracked file that has been deleted from
// the working tree is recorded with an empty hash so deletion is detectable.
func (r *Repo) Snapshot() (Snapshot, error) {
	snap := Snapshot{
		tracked:   make(map[string]string),
		untracked: make(map[string]string),
	}
	ls, err := r.git("ls-files", "-z")
	if err != nil {
		return Snapshot{}, err
	}
	for _, path := range splitZ(ls) {
		hash, err := r.hashFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				snap.tracked[path] = ""
				continue
			}
			return Snapshot{}, err
		}
		snap.tracked[path] = hash
	}
	st, err := r.State()
	if err != nil {
		return Snapshot{}, err
	}
	for _, path := range st.Untracked {
		hash, err := r.hashFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Snapshot{}, err
		}
		snap.untracked[path] = hash
	}
	return snap, nil
}

// Changed returns the repository-relative paths whose content or existence
// differs from snap, ordered deterministically. It reports every file the
// working tree changed relative to the Turn start, before attributing those
// changes to the Agent or to external actors.
func (r *Repo) Changed(snap Snapshot) ([]string, error) {
	changed := make(map[string]bool)

	ls, err := r.git("ls-files", "-z")
	if err != nil {
		return nil, err
	}
	currentTracked := make(map[string]bool)
	for _, path := range splitZ(ls) {
		currentTracked[path] = true
		hash, err := r.hashFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				hash = ""
			} else {
				return nil, err
			}
		}
		if previous, ok := snap.tracked[path]; !ok || previous != hash {
			changed[path] = true
		}
	}
	// Manifest deletion and staged-away tracked files.
	for path := range snap.tracked {
		if !currentTracked[path] {
			changed[path] = true
		}
	}
	// Untracked files present at Turn start.
	for path, previous := range snap.untracked {
		hash, err := r.hashFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				hash = ""
			} else {
				return nil, err
			}
		}
		if previous != hash {
			changed[path] = true
		}
	}
	// Newly created untracked files.
	st, err := r.State()
	if err != nil {
		return nil, err
	}
	for _, path := range st.Untracked {
		if _, existed := snap.untracked[path]; !existed {
			changed[path] = true
		}
	}

	paths := make([]string, 0, len(changed))
	for path := range changed {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

// hashFile returns the SHA-256 digest of the file at repository-relative path.
func (r *Repo) hashFile(path string) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(path)))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Checkpoint commits all pre-existing work as a single user-approved boundary.
// It stages tracked changes with `git add -u` and untracked files individually,
// never using `git add -A`. It returns the new commit's id.
func (r *Repo) Checkpoint(message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("checkpoint message is required")
	}
	st, err := r.State()
	if err != nil {
		return "", err
	}
	if st.Clean {
		return "", errors.New("nothing to checkpoint: working tree is clean")
	}
	if len(st.Staged) > 0 || len(st.Unstaged) > 0 {
		if _, err := r.git("add", "-u"); err != nil {
			return "", fmt.Errorf("stage tracked work: %w", err)
		}
	}
	for _, path := range st.Untracked {
		if _, err := r.git("add", "--", path); err != nil {
			return "", fmt.Errorf("stage untracked file %q: %w", path, err)
		}
	}
	if _, err := r.git("commit", "-m", message); err != nil {
		return "", fmt.Errorf("commit checkpoint: %w", err)
	}
	return r.head()
}

// Stash stashes the working tree, including untracked files, with a message
// that makes the precise stash identifiable in `git stash list`. It returns
// the stable `stash@{N}` reference for the stash it created.
func (r *Repo) Stash(message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("stash message is required")
	}
	if _, err := r.git("stash", "push", "-u", "-m", message); err != nil {
		return "", fmt.Errorf("stash repository: %w", err)
	}
	ref, err := r.findStash(message)
	if err != nil {
		return "", err
	}
	return ref, nil
}

// findStash returns the stash reference whose message matches message.
func (r *Repo) findStash(message string) (string, error) {
	out, err := r.git("stash", "list")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if strings.Contains(line, message) {
			return strings.TrimSpace(strings.SplitN(line, ":", 2)[0]), nil
		}
	}
	return "", fmt.Errorf("stash with message %q not found", message)
}

// Commit stages only the given repository-relative paths and creates a commit
// from them, returning the new commit's id. It never uses `git add -A` and
// never stages pre-existing or externally introduced work.
func (r *Repo) Commit(paths []string, message string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("no files to commit")
	}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			return "", errors.New("agent commit path must not be empty")
		}
	}
	if strings.TrimSpace(message) == "" {
		return "", errors.New("agent commit message is required")
	}
	args := append([]string{"add", "--"}, paths...)
	if _, err := r.git(args...); err != nil {
		return "", fmt.Errorf("stage agent changes: %w", err)
	}
	if _, err := r.git("commit", "-m", message); err != nil {
		return "", fmt.Errorf("create agent commit: %w", err)
	}
	return r.head()
}

// Validate runs the validation command in the repository root and reports
// whether it passed. A command that cannot be started returns an error; a
// command that exits non-zero is a failed Validation with its captured output.
func (r *Repo) Validate(ctx context.Context, command string) (Validation, error) {
	if ctx == nil {
		return Validation{}, errors.New("validation context is nil")
	}
	if strings.TrimSpace(command) == "" {
		return Validation{}, errors.New("validation command is required")
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = r.root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Validation{}, fmt.Errorf("run validation: %w", ctxErr)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return Validation{}, fmt.Errorf("run validation: %w", err)
		}
		return Validation{Command: command, Passed: false, Output: output.String()}, nil
	}
	return Validation{Command: command, Passed: true, Output: output.String()}, nil
}

// head returns the current HEAD commit id.
func (r *Repo) head() (string, error) {
	out, err := r.git("rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// git runs a Git command in the repository root and returns its stdout.
func (r *Repo) git(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", append([]string{"-C", r.root}, args...)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// splitZ splits NUL-separated Git output, trimming a trailing separator.
func splitZ(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(out, "\x00"), "\x00")
}

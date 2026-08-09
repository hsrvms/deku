package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hsrvms/deku/activity"
	"github.com/hsrvms/deku/agent"
	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/repository"
	"github.com/hsrvms/deku/session"
	"github.com/hsrvms/deku/tool"
)

// scriptedDiff is a scripted diffFunc: it serves per-path diffs from a map
// and records the path sets it was asked for, so tests observe the cumulative
// path set the pane passes to the renderer.
type scriptedDiff struct {
	diffs map[string]string
	err   error
	calls [][]string
}

func (s *scriptedDiff) run(paths []string) (map[string]string, error) {
	s.calls = append(s.calls, append([]string(nil), paths...))
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]string, len(paths))
	for _, path := range paths {
		if diff, ok := s.diffs[path]; ok {
			out[path] = diff
		}
	}
	return out, nil
}

func ctrlT() tea.Msg { return tea.KeyMsg{Type: tea.KeyCtrlT} }

// startTurn submits a request like a user would and leaves the Turn pending;
// the returned cmd completes it when the test wants the Turn boundary.
func startTurn(m *Model) tea.Cmd {
	if m.runner == nil {
		m.SetRunner(&stubRunner{})
	}
	typeText(m, "fix it")
	_, cmd := m.Update(enterKey())
	return cmd
}

// diffLinesN builds a git-style unified diff for path with n added lines.
func diffLinesN(path string, prefix string, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
	b.WriteString("--- a/" + path + "\n")
	b.WriteString("+++ b/" + path + "\n")
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", n)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "+%s %03d\n", prefix, i)
	}
	return b.String()
}

func TestTurnDiffAutoOpensOnFirstChange(t *testing.T) {
	diff := &scriptedDiff{diffs: map[string]string{
		"main.go": "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1,2 +1,3 @@\n package main\n-old\n+new\n",
	}}
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	m.diff = diff.run
	cmd := startTurn(m)
	_ = cmd

	if view := stripANSI(m.View()); strings.Contains(view, "Turn Diff") {
		t.Fatal("the pane must stay closed until the first Change of the Turn")
	}
	if m.diffOpen {
		t.Fatal("diffOpen must be false before the first Change")
	}

	m.Change(activity.Change{Tool: "edit", Path: "main.go"})
	view := stripANSI(m.View())
	if !m.diffOpen {
		t.Error("the pane must auto-open on the first Change of the Turn")
	}
	for _, want := range []string{"Turn Diff", "-old", "+new", "diff --git a/main.go b/main.go"} {
		if !strings.Contains(view, want) {
			t.Errorf("Turn Diff pane missing %q, got %q", want, view)
		}
	}
	if got := diff.calls; !reflect.DeepEqual(got, [][]string{{"main.go"}}) {
		t.Errorf("diff runner calls = %#v, want [[main.go]]", got)
	}

	// The pane splits the main area vertically: the diff renders below the
	// Transcript, and the status bar and input line stay at the bottom.
	lines := strings.Split(view, "\n")
	if len(lines) != 48 {
		t.Errorf("View height with the pane open = %d lines, want 48", len(lines))
	}
	if !strings.Contains(lines[len(lines)-2], "tokenrouter/qwen-2.5-coder") {
		t.Errorf("status bar line = %q, want the Selection", lines[len(lines)-2])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "> ") {
		t.Errorf("input line = %q, want the prompt", lines[len(lines)-1])
	}
	diffLine := 0
	for i, line := range lines {
		if strings.HasPrefix(line, "Turn Diff") {
			diffLine = i
		}
	}
	if diffLine == 0 || diffLine >= len(lines)-2 {
		t.Errorf("the Turn Diff pane must render below the Transcript and above the status bar, header at line %d of %d", diffLine, len(lines))
	}
}

func TestTurnDiffExtendsOnSecondEditToSameFile(t *testing.T) {
	diff := &scriptedDiff{diffs: map[string]string{
		"main.go":   "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,2 @@\n-old\n+new\n",
		"notes.txt": "diff --git a/notes.txt b/notes.txt\nnew file mode 100644\n--- /dev/null\n+++ b/notes.txt\n@@ -0,0 +1 @@\n+hello\n",
	}}
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	m.diff = diff.run
	cmd := startTurn(m)
	_ = cmd

	m.Change(activity.Change{Tool: "edit", Path: "main.go"})
	if view := stripANSI(m.View()); !strings.Contains(view, "+new") || strings.Contains(view, "+more") {
		t.Errorf("first render must show the first Edit only, got %q", view)
	}

	// The working tree grew: the second Edit to the same file must extend
	// the first in the pane, not replace it or duplicate the file entry.
	diff.diffs["main.go"] = "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,3 @@\n-old\n+new\n+more\n"
	m.Change(activity.Change{Tool: "edit", Path: "main.go"})
	view := stripANSI(m.View())
	for _, want := range []string{"+new", "+more"} {
		if !strings.Contains(view, want) {
			t.Errorf("second Edit must extend the first, missing %q, got %q", want, view)
		}
	}
	if got := strings.Count(view, "diff --git a/main.go b/main.go"); got != 1 {
		t.Errorf("main.go must render as one cumulative entry, got %d", got)
	}

	// A new file joins the same cumulative diff.
	m.Change(activity.Change{Tool: "write", Path: "notes.txt"})
	view = stripANSI(m.View())
	for _, want := range []string{"+hello", "new file mode 100644", "--- /dev/null"} {
		if !strings.Contains(view, want) {
			t.Errorf("new-file entry missing %q, got %q", want, view)
		}
	}
	want := [][]string{{"main.go"}, {"main.go"}, {"main.go", "notes.txt"}}
	if got := diff.calls; !reflect.DeepEqual(got, want) {
		t.Errorf("diff runner calls = %#v, want %#v", got, want)
	}
}

func TestTurnDiffPerFileCap(t *testing.T) {
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 5000})
	diff := &scriptedDiff{diffs: map[string]string{"big.go": diffLinesN("big.go", "line", 250)}}
	m.diff = diff.run
	cmd := startTurn(m)
	_ = cmd

	m.Change(activity.Change{Tool: "edit", Path: "big.go"})
	view := stripANSI(m.View())
	// 4 header lines plus 196 added lines fit the 200-line cap.
	if !strings.Contains(view, "+line 196") {
		t.Errorf("the pane must show the first 200 diff lines, missing +line 196, got %q", view)
	}
	if strings.Contains(view, "+line 197") {
		t.Errorf("the pane must cap the file diff at 200 lines, got %q", view)
	}
	if !strings.Contains(view, "big.go diff truncated at 200 lines") {
		t.Errorf("per-file truncation note missing, got %q", view)
	}
}

func TestTurnDiffTotalCap(t *testing.T) {
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 5000})
	diffs := make(map[string]string)
	for _, prefix := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		diffs[prefix+".go"] = diffLinesN(prefix+".go", prefix+"line", 150)
	}
	diff := &scriptedDiff{diffs: diffs}
	m.diff = diff.run
	cmd := startTurn(m)
	_ = cmd

	for _, prefix := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		m.Change(activity.Change{Tool: "edit", Path: prefix + ".go"})
	}
	view := stripANSI(m.View())
	// Six files of 154 lines use 924 of the 1000-line budget; the seventh
	// file contributes its 4 header lines and 72 more lines.
	if !strings.Contains(view, "+fline 150") {
		t.Errorf("the sixth file must render in full, got %q", view)
	}
	if !strings.Contains(view, "+gline 072") {
		t.Errorf("the seventh file must fill the remaining budget, got %q", view)
	}
	if strings.Contains(view, "+gline 073") {
		t.Errorf("the total cap must cut the seventh file, got %q", view)
	}
	if !strings.Contains(view, "diff truncated at 1000 lines") {
		t.Errorf("total truncation note missing, got %q", view)
	}
	if strings.Contains(view, "truncated at 200 lines") {
		t.Errorf("no per-file note may appear when only the total cap binds, got %q", view)
	}
}

func TestTurnDiffToggle(t *testing.T) {
	diff := &scriptedDiff{diffs: map[string]string{
		"main.go": "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,2 @@\n-old\n+new\n",
	}}
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	m.diff = diff.run
	cmd := startTurn(m)
	_ = cmd

	m.Change(activity.Change{Tool: "edit", Path: "main.go"})
	if view := m.View(); !strings.Contains(stripANSI(view), "Turn Diff") {
		t.Fatalf("the pane must be open after the first Change, got %q", view)
	}

	m.Update(ctrlT())
	if m.diffOpen {
		t.Error("Ctrl+T must close the pane")
	}
	if view := stripANSI(m.View()); strings.Contains(view, "Turn Diff") {
		t.Errorf("closed pane must not render, got %q", view)
	}

	m.Update(ctrlT())
	if !m.diffOpen {
		t.Error("Ctrl+T must reopen the pane")
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "Turn Diff") || !strings.Contains(view, "+new") {
		t.Errorf("reopened pane must show the cached diff, got %q", view)
	}
	if got := diff.calls; !reflect.DeepEqual(got, [][]string{{"main.go"}}) {
		t.Errorf("reopening must not recompute the diff, calls = %#v", got)
	}
}

func TestTurnDiffPersistsAcrossTurnBoundary(t *testing.T) {
	diff := &scriptedDiff{diffs: map[string]string{
		"main.go":   "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,2 @@\n-old\n+new\n",
		"notes.txt": "diff --git a/notes.txt b/notes.txt\n--- a/notes.txt\n+++ b/notes.txt\n@@ -1 +1,2 @@\n-orig\n+hello\n",
	}}
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	m.diff = diff.run
	m.SetRunner(&stubRunner{})

	cmd := startTurn(m)
	m.Change(activity.Change{Tool: "edit", Path: "main.go"})
	if view := stripANSI(m.View()); !strings.Contains(view, "+new") {
		t.Fatalf("the pane must open during the Turn, got %q", view)
	}

	// A completed Turn leaves the pane showing its diff.
	completeTurn(t, m, cmd)
	if m.turnActive {
		t.Fatal("Turn must finish when the result arrives")
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "Turn Diff") || !strings.Contains(view, "+new") {
		t.Errorf("the pane must persist after the Turn completes, got %q", view)
	}

	// The next Turn starts a fresh pane: closed, with an empty path set.
	typeText(m, "again")
	_, cmd = m.Update(enterKey())
	if cmd == nil {
		t.Fatal("the second request must start a Turn")
	}
	if m.diffOpen {
		t.Error("a new Turn must close the completed Turn's pane")
	}
	if view := stripANSI(m.View()); strings.Contains(view, "Turn Diff") {
		t.Errorf("the pane must not render before the new Turn's first Change, got %q", view)
	}

	// The new Turn's first Change re-opens the pane with only its own paths.
	m.Change(activity.Change{Tool: "edit", Path: "notes.txt"})
	view := stripANSI(m.View())
	if !strings.Contains(view, "+hello") {
		t.Errorf("the new Turn's pane must render its own diff, got %q", view)
	}
	if strings.Contains(view, "-old") {
		t.Errorf("the previous Turn's diff must not leak into the new Turn, got %q", view)
	}
	want := [][]string{{"main.go"}, {"notes.txt"}}
	if got := diff.calls; !reflect.DeepEqual(got, want) {
		t.Errorf("diff runner calls = %#v, want %#v", got, want)
	}
}

func TestTurnDiffDoesNotOpenOutsideATurn(t *testing.T) {
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	m.diff = (&scriptedDiff{}).run
	m.Change(activity.Change{Tool: "edit", Path: "main.go"})

	if m.diffOpen {
		t.Error("a Change outside a Turn must not open the pane")
	}
	if view := stripANSI(m.View()); strings.Contains(view, "Turn Diff") {
		t.Errorf("the pane must not render outside a Turn, got %q", view)
	}
	if got := m.Changes(); len(got) != 1 || got[0].Path != "main.go" {
		t.Errorf("Changes() = %#v, want the recorded Change", got)
	}
}

func TestTurnDiffErrorNote(t *testing.T) {
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	m.diff = func([]string) (map[string]string, error) {
		return nil, errors.New("not a Git repository")
	}
	cmd := startTurn(m)
	_ = cmd

	m.Change(activity.Change{Tool: "edit", Path: "main.go"})
	if view := stripANSI(m.View()); !strings.Contains(view, "diff unavailable: not a Git repository") {
		t.Errorf("the pane must report the diff failure, got %q", view)
	}
}

func TestGitDiffRunnerUsesWorkingDirectoryRepository(t *testing.T) {
	dir := initGitRepo(t)
	commitGitFile(t, dir, "a.txt", "one\ntwo\n")
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nCHANGED\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\nfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	diffs, err := gitDiff()([]string{"a.txt", "b.txt"})
	if err != nil {
		t.Fatalf("gitDiff() error = %v", err)
	}
	if !strings.Contains(diffs["a.txt"], "+CHANGED") {
		t.Errorf("a.txt diff = %q, want the tracked modification", diffs["a.txt"])
	}
	for _, want := range []string{"new file mode 100644", "+new", "+file"} {
		if !strings.Contains(diffs["b.txt"], want) {
			t.Errorf("b.txt diff missing %q, got %q", want, diffs["b.txt"])
		}
	}
}

// initGitRepo creates a temporary Git repository with an initial empty commit
// and returns its root.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return dir
}

// commitGitFile commits a file at a repository-relative path.
func commitGitFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "--", path}, {"commit", "-q", "-m", "commit " + path}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
}

// TestRealAgentTurnRendersRealGitDiffEndToEnd runs a real Agent through the
// shell in a real Git repository: an approved Write must auto-open the Turn
// Diff pane and render the new file as a full-content addition computed from
// the working tree with git — the whole Agent → Change → git diff → pane path
// a user drives.
func TestRealAgentTurnRendersRealGitDiffEndToEnd(t *testing.T) {
	root := initGitRepo(t)
	commitGitFile(t, root, "tracked.txt", "keep\n")

	registry, err := tool.NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	conversation, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	providerStub := &scriptedProvider{
		responses: [][]provider.Event{
			{provider.ToolCall{ID: "call-1", Name: "write", Arguments: `{"path":"notes.txt","content":"hello\nworld\n"}`}, provider.Done{}},
			{provider.TextDelta{Text: "Created notes.txt."}, provider.Done{}},
		},
	}
	approvalReader, approvalWriter := io.Pipe()
	m := New("tokenrouter", "qwen-2.5-coder", approvalWriter)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	repo, err := repository.New(root)
	if err != nil {
		t.Fatalf("repository.New() error = %v", err)
	}
	m.diff = repo.Diff
	runner := agent.NewWithActivity(providerStub, "qwen-2.5-coder", conversation, m, approvalReader, registry, approval.DefaultPolicy(), nil, m)
	m.SetRunner(runner)

	typeText(m, "create notes.txt")
	_, cmd := m.Update(enterKey())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = approvalWriter.Write([]byte("y\n")) // the gate blocks until this arrives
	}()
	completeTurn(t, m, cmd)
	<-done

	view := stripANSI(m.View())
	if !m.diffOpen {
		t.Error("the pane must auto-open on the Write's Change event")
	}
	for _, want := range []string{
		"Turn Diff",
		"diff --git a/notes.txt b/notes.txt",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/notes.txt",
		"+hello",
		"+world",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("Turn Diff pane missing %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "tracked.txt") {
		t.Errorf("unchanged tracked files must not appear in the diff, got %q", view)
	}
}

func TestTurnDiffPaneWindowShowsTopOfTallDiff(t *testing.T) {
	diff := &scriptedDiff{diffs: map[string]string{"big.go": diffLinesN("big.go", "line", 50)}}
	m := newTestModel(io.Discard)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 24}) // pane window is 7 lines
	m.diff = diff.run
	cmd := startTurn(m)
	_ = cmd

	m.Change(activity.Change{Tool: "edit", Path: "big.go"})
	view := stripANSI(m.View())
	// The pane window shows the top of the diff: the header, the rule, and
	// the first 5 diff lines; the frame never grows past the terminal.
	lines := strings.Split(view, "\n")
	if len(lines) != 24 {
		t.Errorf("View height = %d lines, want 24 (pane window + status + input)", len(lines))
	}
	if !strings.Contains(view, "+line 001") {
		t.Errorf("the pane must show the top of the diff, got %q", view)
	}
	if strings.Contains(view, "+line 050") {
		t.Errorf("the pane must not show lines below its window, got %q", view)
	}
}

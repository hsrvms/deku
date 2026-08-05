package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo creates a temporary Git repository with an initial empty commit and
// returns its root. Tests commit and mutate it through real Git commands so the
// module is verified against actual Git behavior.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q")
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("run %s %s: %v: %s", name, strings.Join(args, " "), err, out.String())
	}
	return out.String()
}

func commitFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", "--", path)
	run(t, dir, "git", "commit", "-q", "-m", "commit "+path)
}

func TestParseMode(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{"off", ModeOff, false},
		{"ask", ModeAsk, false},
		{"on", ModeOn, false},
		{"ON", ModeOn, false},
		{" Ask ", ModeAsk, false},
		{"auto", "", true},
		{"", "", true},
	} {
		got, err := ParseMode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseMode(%q) error = nil, want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMode(%q) error = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestModeValid(t *testing.T) {
	if !ModeOff.Valid() || !ModeAsk.Valid() || !ModeOn.Valid() {
		t.Fatal("off, ask, on must all be valid modes")
	}
	if (Mode("auto")).Valid() {
		t.Fatal("unknown mode must be invalid")
	}
}

func TestNewRejectsNonRepository(t *testing.T) {
	dir := t.TempDir()
	if _, err := New(dir); err == nil {
		t.Fatal("New() on a non-repository should fail")
	}
}

func TestStateClean(t *testing.T) {
	dir := initRepo(t)
	commitFile(t, dir, "main.go", "package main\n")
	repo, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	st, err := repo.State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if !st.Clean {
		t.Errorf("State() = %+v, want clean", st)
	}
	if st.Dirty() {
		t.Errorf("State() dirty, want clean")
	}
}

func TestStateDirty(t *testing.T) {
	dir := initRepo(t)
	commitFile(t, dir, "main.go", "package main\n")
	commitFile(t, dir, "other.go", "package main\n")
	// staged change
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc x() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", "--", "main.go")
	// unstaged change
	if err := os.WriteFile(filepath.Join(dir, "other.go"), []byte("package main\n\nfunc y() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// untracked file
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	repo, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	st, err := repo.State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if st.Clean {
		t.Fatal("State() clean, want dirty")
	}
	if len(st.Staged) != 1 || st.Staged[0] != "main.go" {
		t.Errorf("staged = %#v, want [main.go]", st.Staged)
	}
	if len(st.Unstaged) != 1 || st.Unstaged[0] != "other.go" {
		t.Errorf("unstaged = %#v, want [other.go]", st.Unstaged)
	}
	if len(st.Untracked) != 1 || st.Untracked[0] != "new.txt" {
		t.Errorf("untracked = %#v, want [new.txt]", st.Untracked)
	}
}

func TestSnapshotAndChanged(t *testing.T) {
	dir := initRepo(t)
	commitFile(t, dir, "main.go", "package main\n")
	repo, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	snap, err := repo.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	// Agent edits a tracked file and creates a new untracked file.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc run() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := repo.Changed(snap)
	if err != nil {
		t.Fatalf("Changed() error = %v", err)
	}
	got := strings.Join(changed, ",")
	if !strings.Contains(got, "main.go") {
		t.Errorf("Changed() = %q, want main.go", got)
	}
	if !strings.Contains(got, "notes.txt") {
		t.Errorf("Changed() = %q, want notes.txt", got)
	}
}

func TestSnapshotNoChanges(t *testing.T) {
	dir := initRepo(t)
	commitFile(t, dir, "main.go", "package main\n")
	repo, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	snap, err := repo.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	changed, err := repo.Changed(snap)
	if err != nil {
		t.Fatalf("Changed() error = %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("Changed() = %#v, want no changes", changed)
	}
}

func TestCheckpointPreservesAllWork(t *testing.T) {
	dir := initRepo(t)
	commitFile(t, dir, "main.go", "package main\n")
	// pre-existing dirty work: one tracked modification, one untracked file.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc existing() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("wip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sha, err := repo.Checkpoint("deku: checkpoint pre-existing work before agent turn")
	if err != nil {
		t.Fatalf("Checkpoint() error = %v", err)
	}
	if strings.TrimSpace(sha) == "" {
		t.Fatal("Checkpoint() returned empty commit id")
	}
	st, err := repo.State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if !st.Clean {
		t.Errorf("State() after checkpoint = %+v, want clean", st)
	}
	// The checkpoint should contain both the modified and untracked file.
	ls := run(t, dir, "git", "show", "--stat", "--format=", "HEAD")
	if !strings.Contains(ls, "main.go") || !strings.Contains(ls, "dirty.txt") {
		t.Errorf("checkpoint stat = %q, want main.go and dirty.txt", ls)
	}
}

func TestStashCreatesIdentifiableStash(t *testing.T) {
	dir := initRepo(t)
	commitFile(t, dir, "main.go", "package main\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc wip() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	const message = "deku: pre-existing work stashed before agent turn 20260802T000000Z"
	ref, err := repo.Stash(message)
	if err != nil {
		t.Fatalf("Stash() error = %v", err)
	}
	if !strings.HasPrefix(ref, "stash@{") {
		t.Errorf("stash ref = %q, want a stash@{N} reference", ref)
	}
	// The message must be identifiable in the stash list.
	list := run(t, dir, "git", "stash", "list")
	if !strings.Contains(list, message) {
		t.Errorf("stash list = %q, want message %q", list, message)
	}
	// The working tree should now be clean.
	st, err := repo.State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if !st.Clean {
		t.Errorf("State() after stash = %+v, want clean", st)
	}
}

func TestCommitStagesOnlyGivenPaths(t *testing.T) {
	dir := initRepo(t)
	commitFile(t, dir, "main.go", "package main\n")
	commitFile(t, dir, "other.go", "package main\n")
	// Agent modifies main.go only.
	agentPath := "main.go"
	if err := os.WriteFile(filepath.Join(dir, agentPath), []byte("package main\n\nfunc run() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A pre-existing untracked file must NOT be absorbed by the Agent Commit.
	if err := os.WriteFile(filepath.Join(dir, "pre_existing.txt"), []byte("keep me unsaved\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sha, err := repo.Commit([]string{agentPath}, "deku: agent changes")
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if strings.TrimSpace(sha) == "" {
		t.Fatal("Commit() returned empty commit id")
	}
	stat := run(t, dir, "git", "show", "--stat", "--format=", "HEAD")
	if strings.Contains(stat, "pre_existing.txt") {
		t.Errorf("Agent Commit absorbed pre-existing untracked file: %q", stat)
	}
	if !strings.Contains(stat, "main.go") {
		t.Errorf("Agent Commit stat = %q, want main.go", stat)
	}
	// The pre-existing file must remain untracked.
	st, err := repo.State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if len(st.Untracked) != 1 || st.Untracked[0] != "pre_existing.txt" {
		t.Errorf("untracked after commit = %#v, want [pre_existing.txt]", st.Untracked)
	}
}

func TestValidate(t *testing.T) {
	dir := initRepo(t)
	repo, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	passed := false
	validation, err := repo.Validate(t.Context(), "true")
	if err != nil {
		t.Fatalf("Validate(true) error = %v", err)
	}
	if !validation.Passed {
		t.Errorf("Validate(true).Passed = false, want true")
	}
	_ = passed
	failing, err := repo.Validate(t.Context(), "false")
	if err != nil {
		t.Fatalf("Validate(false) error = %v", err)
	}
	if failing.Passed {
		t.Errorf("Validate(false).Passed = true, want false")
	}
	if failing.Command != "false" {
		t.Errorf("Validate(false).Command = %q, want false", failing.Command)
	}
}

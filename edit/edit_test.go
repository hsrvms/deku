package edit

import (
	"reflect"
	"testing"
)

func TestApplyReplacesExactSingleMatch(t *testing.T) {
	input := []byte("package main\n\nfunc main() {}\n")
	changes := []Change{{OldText: "func main() {}\n", NewText: "func main() { println(\"hi\") }\n"}}
	got, err := Apply(input, changes)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := "package main\n\nfunc main() { println(\"hi\") }\n"
	if string(got) != want {
		t.Errorf("Apply() = %q, want %q", got, want)
	}
}

func TestApplyAppliesMultipleNonOverlappingChanges(t *testing.T) {
	input := []byte("package main\n\nfunc main() {}\n")
	changes := []Change{
		{OldText: "package main", NewText: "package app"},
		{OldText: "func main() {}", NewText: "func run() {}"},
	}
	got, err := Apply(input, changes)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := "package app\n\nfunc run() {}\n"
	if string(got) != want {
		t.Errorf("Apply() = %q, want %q", got, want)
	}
}

func TestApplyRejectsMissingOldTextAndLeavesDataUnchanged(t *testing.T) {
	input := []byte("package main\n")
	changes := []Change{{OldText: "func missing()", NewText: "func replaced()"}}
	got, err := Apply(input, changes)
	if err == nil {
		t.Fatal("Apply() error = nil, want missing-match error")
	}
	if !reflect.DeepEqual(got, input) {
		t.Errorf("Apply() mutated data on failure = %q, want %q", got, input)
	}
}

func TestApplyRejectsNonUniqueOldTextAndLeavesDataUnchanged(t *testing.T) {
	input := []byte("value = 1\nvalue = 2\n")
	changes := []Change{{OldText: "value = ", NewText: "item = "}}
	got, err := Apply(input, changes)
	if err == nil {
		t.Fatal("Apply() error = nil, want non-unique-match error")
	}
	if !reflect.DeepEqual(got, input) {
		t.Errorf("Apply() mutated data on failure = %q, want %q", got, input)
	}
}

func TestApplyIsAtomicWhenLaterChangeFails(t *testing.T) {
	input := []byte("package main\n\nfunc main() {}\n")
	changes := []Change{
		{OldText: "package main", NewText: "package app"},
		{OldText: "func absent()", NewText: "func added()"},
	}
	got, err := Apply(input, changes)
	if err == nil {
		t.Fatal("Apply() error = nil, want missing-match error")
	}
	if !reflect.DeepEqual(got, input) {
		t.Errorf("Apply() partially applied on failure = %q, want %q", got, input)
	}
}

func TestApplyRejectsOverlappingChanges(t *testing.T) {
	input := []byte("foobar\n")
	changes := []Change{
		{OldText: "foo", NewText: "x"},
		{OldText: "foobar", NewText: "y"},
	}
	if _, err := Apply(input, changes); err == nil {
		t.Fatal("Apply() error = nil, want overlap error")
	}
}

func TestApplyRejectsEmptyOldText(t *testing.T) {
	input := []byte("package main\n")
	changes := []Change{{OldText: "", NewText: "x"}}
	if _, err := Apply(input, changes); err == nil {
		t.Fatal("Apply() error = nil, want empty oldText error")
	}
}

func TestApplyReportsWhichChangeFailed(t *testing.T) {
	input := []byte("package main\n")
	changes := []Change{
		{OldText: "package main", NewText: "package app"},
		{OldText: "func absent()", NewText: "func added()"},
	}
	_, err := Apply(input, changes)
	if err == nil {
		t.Fatal("Apply() error = nil, want failure")
	}
	if err.Error() != "edit 2: oldText not found" {
		t.Errorf("Apply() error message = %q, want %q", err.Error(), "edit 2: oldText not found")
	}
}

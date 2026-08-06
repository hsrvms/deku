package lineio

import (
	"bufio"
	"strings"
	"testing"
)

func reader(s string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(s))
}

func TestScanSkipsBlankLines(t *testing.T) {
	br := reader("\n\n  hello  \n")
	got, err := Scan(br)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if got != "hello" {
		t.Errorf("Scan() = %q, want %q", got, "hello")
	}
}

func TestScanTrimsWhitespace(t *testing.T) {
	br := reader("  yes  \n")
	got, err := Scan(br)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if got != "yes" {
		t.Errorf("Scan() = %q, want yes", got)
	}
}

func TestScanEndsBeforeNonEmptyLine(t *testing.T) {
	br := reader("\n\n")
	_, err := Scan(br)
	if err == nil {
		t.Fatal("Scan() error = nil, want end-of-input error")
	}
	if !strings.Contains(err.Error(), "ended before a response") {
		t.Errorf("Scan() error = %q, want a clear end-of-input message", err)
	}
}

func TestScanEmptyInput(t *testing.T) {
	br := reader("")
	_, err := Scan(br)
	if err == nil {
		t.Fatal("Scan() error = nil, want end-of-input error")
	}
}

func TestReadLineReturnsPartialOnEOF(t *testing.T) {
	// Data without a trailing newline is still a complete line.
	br := reader("answer")
	line, err := ReadLine(br)
	if err != nil {
		t.Fatalf("ReadLine() error = %v", err)
	}
	if line != "answer" {
		t.Errorf("ReadLine() = %q, want %q", line, "answer")
	}
}

func TestReadLineReassemblesLongLine(t *testing.T) {
	// A line longer than the reader's buffer must be returned whole, not in
	// fragments, so a long Approval or Agent decision is not truncated.
	long := strings.Repeat("x", 4096) + "\n"
	br := reader(long)
	line, err := ReadLine(br)
	if err != nil {
		t.Fatalf("ReadLine() error = %v", err)
	}
	if line != long {
		t.Errorf("ReadLine() length = %d, want %d", len(line), len(long))
	}
	if !strings.HasSuffix(line, "\n") {
		t.Errorf("ReadLine() = %q, want trailing newline preserved", line)
	}
}

func TestScanLongLine(t *testing.T) {
	long := strings.Repeat("y", 4096) + "\n"
	br := reader("  " + long + "  \n")
	got, err := Scan(br)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if got != strings.TrimSpace(long) {
		t.Errorf("Scan() length = %d, want %d", len(got), len(strings.TrimSpace(long)))
	}
}

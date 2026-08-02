package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreCreatesAppendsAndResumesSession(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	created, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create() returned an empty session ID")
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("Create() returned a zero creation timestamp")
	}

	messages := []Message{
		{Role: RoleUser, Content: "Explain this repository."},
		{Role: RoleAssistant, Content: "It is a small Go application."},
	}
	for _, message := range messages {
		if err := created.Append(message); err != nil {
			t.Fatalf("Append(%#v) error = %v", message, err)
		}
	}

	data, err := os.ReadFile(created.Path())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	lines := splitJSONLines(data)
	if len(lines) != len(messages) {
		t.Fatalf("session file has %d JSONL records, want %d", len(lines), len(messages))
	}
	for index, line := range lines {
		var got Message
		if err := json.Unmarshal(line, &got); err != nil {
			t.Fatalf("JSONL record %d is invalid: %v", index, err)
		}
		if got != messages[index] {
			t.Errorf("JSONL record %d = %#v, want %#v", index, got, messages[index])
		}
	}

	resumed, err := store.Resume(created.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.ID != created.ID {
		t.Errorf("resumed ID = %q, want %q", resumed.ID, created.ID)
	}
	if !resumed.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("resumed CreatedAt = %s, want %s", resumed.CreatedAt, created.CreatedAt)
	}
	if len(resumed.Messages) != len(messages) {
		t.Fatalf("resumed message count = %d, want %d", len(resumed.Messages), len(messages))
	}
	for index, message := range resumed.Messages {
		if message != messages[index] {
			t.Errorf("resumed message %d = %#v, want %#v", index, message, messages[index])
		}
	}
}

func TestStoreCreatesUniqueSessions(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	first, err := store.Create()
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	second, err := store.Create()
	if err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("two sessions have the same ID %q", first.ID)
	}
	if first.Path() == second.Path() {
		t.Fatalf("two sessions have the same path %q", first.Path())
	}
}

func TestStoreRejectsInvalidSessionIDs(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	for _, id := range []string{"", ".", "..", "../other", filepath.Join("nested", "session")} {
		if _, err := store.Resume(id); err == nil {
			t.Errorf("Resume(%q) error = nil, want validation error", id)
		}
	}
}

func splitJSONLines(data []byte) [][]byte {
	var lines [][]byte
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

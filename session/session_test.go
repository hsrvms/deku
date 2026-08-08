package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hsrvms/deku/provider"
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
		if !reflect.DeepEqual(got, messages[index]) {
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
		if !reflect.DeepEqual(message, messages[index]) {
			t.Errorf("resumed message %d = %#v, want %#v", index, message, messages[index])
		}
	}
}

func TestSessionPersistsToolCallsAndResults(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	created, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	messages := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "read", Arguments: `{"path":"main.go"}`}}, Content: ""},
		{Role: RoleTool, ToolCallID: "call-1", Name: "read", Content: "package main\n"},
	}
	for _, message := range messages {
		if err := created.Append(message); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	resumed, err := store.Resume(created.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if !reflect.DeepEqual(resumed.Messages, messages) {
		t.Fatalf("resumed messages = %#v, want %#v", resumed.Messages, messages)
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

func TestSessionRecordsAndRestoresSelection(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	created, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := created.Append(Message{Role: RoleUser, Content: "hello"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := created.RecordSelection(provider.Selection{Provider: "openrouter", Model: "model-a"}); err != nil {
		t.Fatalf("RecordSelection() error = %v", err)
	}

	// The in-memory session reports the override immediately.
	got, ok := created.LatestSelection()
	if !ok || got != (provider.Selection{Provider: "openrouter", Model: "model-a"}) {
		t.Errorf("LatestSelection() = %#v, %v; want the recorded override", got, ok)
	}

	resumed, err := store.Resume(created.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	// The Selection record is not a conversation message.
	if len(resumed.Messages) != 1 || resumed.Messages[0].Content != "hello" {
		t.Errorf("resumed messages = %#v, want only the conversation message", resumed.Messages)
	}
	got, ok = resumed.LatestSelection()
	if !ok || got != (provider.Selection{Provider: "openrouter", Model: "model-a"}) {
		t.Errorf("resumed LatestSelection() = %#v, %v; want the override restored", got, ok)
	}
}

func TestSessionLatestSelectionIsTheLastRecorded(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	created, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := created.RecordSelection(provider.Selection{Provider: "first", Model: "model-a"}); err != nil {
		t.Fatalf("RecordSelection() error = %v", err)
	}
	if err := created.RecordSelection(provider.Selection{Provider: "second", Model: "model-b"}); err != nil {
		t.Fatalf("RecordSelection() error = %v", err)
	}

	resumed, err := store.Resume(created.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	got, ok := resumed.LatestSelection()
	if !ok || got != (provider.Selection{Provider: "second", Model: "model-b"}) {
		t.Errorf("LatestSelection() = %#v, %v; want the last recorded override", got, ok)
	}
}

func TestSessionSelectionInterleavesWithMessages(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	created, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := created.Append(Message{Role: RoleUser, Content: "first"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := created.RecordSelection(provider.Selection{Provider: "other", Model: "model-x"}); err != nil {
		t.Fatalf("RecordSelection() error = %v", err)
	}
	if err := created.Append(Message{Role: RoleAssistant, Content: "second"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	resumed, err := store.Resume(created.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if len(resumed.Messages) != 2 {
		t.Fatalf("resumed messages = %#v, want both conversation messages", resumed.Messages)
	}
	if got, ok := resumed.LatestSelection(); !ok || got.Provider != "other" {
		t.Errorf("resumed LatestSelection() = %#v, %v; want the interleaved override", got, ok)
	}
}

func TestSessionRecordSelectionValidates(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	created, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for _, invalid := range []provider.Selection{
		{},
		{Provider: "provider-only"},
		{Model: "model-only"},
		{Provider: "  ", Model: "model-a"},
	} {
		if err := created.RecordSelection(invalid); err == nil {
			t.Errorf("RecordSelection(%#v) error = nil, want validation error", invalid)
		}
	}
	if _, ok := created.LatestSelection(); ok {
		t.Error("LatestSelection() after invalid records = ok, want none")
	}
	// Nothing was persisted.
	data, err := os.ReadFile(created.Path())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(splitJSONLines(data)) != 0 {
		t.Errorf("session file has records after invalid RecordSelection calls, want none")
	}
}

func TestSessionResumesLegacyTranscriptWithoutSelection(t *testing.T) {
	// Transcripts written before Selection records existed hold bare message
	// lines; they resume unchanged and report no override.
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	created, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	legacy := `{"role":"user","content":"legacy request"}
{"role":"assistant","content":"legacy response"}
`
	if err := os.WriteFile(created.Path(), []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	resumed, err := store.Resume(created.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if len(resumed.Messages) != 2 {
		t.Fatalf("resumed messages = %#v, want both legacy messages", resumed.Messages)
	}
	if _, ok := resumed.LatestSelection(); ok {
		t.Error("LatestSelection() on a legacy transcript = ok, want none")
	}
}

func TestSessionResumeRejectsUnknownRecordType(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	created, err := store.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := os.WriteFile(created.Path(), []byte(`{"type":"future","future":{}}`+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := store.Resume(created.ID); err == nil {
		t.Fatal("expected error for an unknown transcript record type")
	}
}

// Package session persists an immutable, append-only JSONL transcript and
// reconstructs Session history when resumed. The transcript holds the
// conversation messages and per-Session Selection records (the Selection
// value itself is owned by the provider module — Session records it, it does
// not define it); messages written before Selection records existed are bare
// message lines and resume unchanged.
package session

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hsrvms/deku/provider"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"

	sessionIDLayout = "20060102T150405.000000000Z"
)

// ToolCall is the provider-independent transcript form of a model Tool Call.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message is one immutable entry in a Session's conversation log.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// Transcript record types. Message lines carry no type: they predate the
// typed records and are read as messages when the type is absent.
const (
	recordTypeSelection = "selection"
)

// transcriptRecord is one typed JSONL line. Exactly one payload is set for a
// valid record. The recorded Selection value is owned by the provider module;
// the Session persists it.
type transcriptRecord struct {
	Type      string              `json:"type"`
	Selection *provider.Selection `json:"selection,omitempty"`
}

// Session is an append-only conversation stored by a Store.
type Session struct {
	ID        string
	CreatedAt time.Time
	Messages  []Message

	selection *provider.Selection

	store *Store
	mu    sync.Mutex
}

// Path returns the JSONL file used to persist the Session.
func (s *Session) Path() string {
	if s == nil || s.store == nil {
		return ""
	}
	return filepath.Join(s.store.dir, s.ID+".jsonl")
}

// Append persists message as one new JSONL record and adds it to the in-memory
// history only after the file write succeeds.
func (s *Session) Append(message Message) error {
	if err := s.writable(); err != nil {
		return err
	}
	if err := validateMessage(message); err != nil {
		return err
	}

	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode session message: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.appendRecord(data); err != nil {
		return err
	}
	s.Messages = append(s.Messages, message)
	return nil
}

// RecordSelection appends a Selection record to the transcript, making it the
// Session's active override. The record is immutable once written; a later
// RecordSelection supersedes it. Both the Provider and the Model are
// required: a partial override is never recorded.
func (s *Session) RecordSelection(selection provider.Selection) error {
	if err := s.writable(); err != nil {
		return err
	}
	if strings.TrimSpace(selection.Provider) == "" {
		return errors.New("selection provider is required")
	}
	if strings.TrimSpace(selection.Model) == "" {
		return errors.New("selection model is required")
	}

	record := transcriptRecord{Type: recordTypeSelection, Selection: &selection}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session selection: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.appendRecord(data); err != nil {
		return err
	}
	s.selection = &selection
	return nil
}

// writable validates that the Session can accept a new transcript record.
func (s *Session) writable() error {
	if s == nil {
		return errors.New("session is nil")
	}
	if s.store == nil {
		return errors.New("session has no store")
	}
	return validateSessionID(s.ID)
}

// appendRecord writes one JSONL record line to the Session file. The caller
// must hold s.mu.
func (s *Session) appendRecord(data []byte) error {
	file, err := os.OpenFile(s.Path(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("open session %q for append: %w", s.ID, err)
	}
	written, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		return fmt.Errorf("append session %q: %w", s.ID, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close session %q: %w", s.ID, closeErr)
	}
	return nil
}

// LatestSelection returns the last Selection recorded in the Session, whether
// during this run or restored on resume. The boolean reports whether any
// override exists; without one the configured default Selection applies.
func (s *Session) LatestSelection() (provider.Selection, bool) {
	if s == nil {
		return provider.Selection{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selection == nil {
		return provider.Selection{}, false
	}
	return *s.selection, true
}

// Store owns Session files in one directory.
type Store struct {
	dir string
}

// NewStore creates a Session store rooted at dir. Store and Session files are
// private to the current user.
func NewStore(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("session store directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create session store %q: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// DefaultStore returns the store under ~/.deku/sessions.
func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home directory for sessions: %w", err)
	}
	return NewStore(filepath.Join(home, ".deku", "sessions"))
}

// Dir returns the directory containing the store's Session files.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// Create creates a new empty Session with a timestamped, unique ID.
func (s *Store) Create() (*Session, error) {
	if s == nil {
		return nil, errors.New("session store is nil")
	}

	for attempt := 0; attempt < 10; attempt++ {
		createdAt := time.Now().UTC()
		id, err := newSessionID(createdAt)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(s.dir, id+".jsonl")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, fmt.Errorf("create session %q: %w", id, err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close new session %q: %w", id, err)
		}
		return &Session{
			ID:        id,
			CreatedAt: createdAt,
			Messages:  make([]Message, 0),
			store:     s,
		}, nil
	}

	return nil, errors.New("create session: could not generate a unique ID")
}

// Resume opens an existing Session and reconstructs its complete transcript:
// the conversation messages and the latest recorded Selection override.
func (s *Store) Resume(id string) (resumed *Session, err error) {
	if s == nil {
		return nil, errors.New("session store is nil")
	}
	if err := validateSessionID(id); err != nil {
		return nil, err
	}

	path := filepath.Join(s.dir, id+".jsonl")
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session %q: %w", id, err)
	}
	defer func() {
		closeErr := file.Close()
		if closeErr == nil {
			return
		}
		closeErr = fmt.Errorf("close session %q: %w", id, closeErr)
		if err == nil {
			resumed = nil
			err = closeErr
			return
		}
		err = errors.Join(err, closeErr)
	}()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat session %q: %w", id, err)
	}

	messages, selection, err := readTranscript(file, id)
	if err != nil {
		return nil, err
	}

	createdAt := createdAtFromID(id)
	if createdAt.IsZero() {
		createdAt = info.ModTime().UTC()
	}
	return &Session{
		ID:        id,
		CreatedAt: createdAt,
		Messages:  messages,
		selection: selection,
		store:     s,
	}, nil
}

// readTranscript reconstructs the conversation messages and the latest
// Selection record from a Session file. Lines without a record type are
// conversation messages; a line with an unknown type fails the resume so a
// corrupted transcript is never silently truncated.
func readTranscript(reader io.Reader, id string) ([]Message, *provider.Selection, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	messages := make([]Message, 0)
	var latest *provider.Selection
	line := 0
	for scanner.Scan() {
		line++
		data := strings.TrimSpace(scanner.Text())
		if data == "" {
			continue
		}
		var record transcriptRecord
		if err := json.Unmarshal([]byte(data), &record); err != nil {
			return nil, nil, fmt.Errorf("decode session %q record %d: %w", id, line, err)
		}
		switch record.Type {
		case "":
			var message Message
			if err := json.Unmarshal([]byte(data), &message); err != nil {
				return nil, nil, fmt.Errorf("decode session %q record %d: %w", id, line, err)
			}
			if err := validateMessage(message); err != nil {
				return nil, nil, fmt.Errorf("validate session %q record %d: %w", id, line, err)
			}
			messages = append(messages, message)
		case recordTypeSelection:
			if record.Selection == nil || strings.TrimSpace(record.Selection.Provider) == "" || strings.TrimSpace(record.Selection.Model) == "" {
				return nil, nil, fmt.Errorf("validate session %q record %d: selection record requires a provider and a model", id, line)
			}
			selection := *record.Selection
			latest = &selection
		default:
			return nil, nil, fmt.Errorf("session %q record %d has unknown type %q", id, line, record.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("read session %q: %w", id, err)
	}
	return messages, latest, nil
}

func validateMessage(message Message) error {
	if message.Role != RoleUser && message.Role != RoleAssistant && message.Role != RoleTool {
		return fmt.Errorf("session message role must be %q, %q, or %q, got %q", RoleUser, RoleAssistant, RoleTool, message.Role)
	}
	if message.Role == RoleTool && strings.TrimSpace(message.ToolCallID) == "" {
		return errors.New("tool session message requires a tool call ID")
	}
	for _, call := range message.ToolCalls {
		if strings.TrimSpace(call.Name) == "" {
			return errors.New("session tool call name is required")
		}
	}
	return nil
}

func validateSessionID(id string) error {
	if id == "" || id == "." || id == ".." {
		return errors.New("session ID is required")
	}
	if filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return fmt.Errorf("invalid session ID %q", id)
	}
	return nil
}

func newSessionID(createdAt time.Time) (string, error) {
	var suffix [8]byte
	if _, err := io.ReadFull(rand.Reader, suffix[:]); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return createdAt.UTC().Format(sessionIDLayout) + "-" + hex.EncodeToString(suffix[:]), nil
}

func createdAtFromID(id string) time.Time {
	prefix, _, ok := strings.Cut(id, "-")
	if !ok {
		return time.Time{}
	}
	createdAt, err := time.Parse(sessionIDLayout, prefix)
	if err != nil {
		return time.Time{}
	}
	return createdAt.UTC()
}

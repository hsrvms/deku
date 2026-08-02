// Package session persists an immutable, append-only JSONL message log and
// reconstructs Session history when resumed.
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
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"

	sessionIDLayout = "20060102T150405.000000000Z"
)

// Message is one immutable entry in a Session's conversation log.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Session is an append-only conversation stored by a Store.
type Session struct {
	ID        string
	CreatedAt time.Time
	Messages  []Message

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
	if s == nil {
		return errors.New("session is nil")
	}
	if s.store == nil {
		return errors.New("session has no store")
	}
	if err := validateSessionID(s.ID); err != nil {
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

	s.Messages = append(s.Messages, message)
	return nil
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

// Resume opens an existing Session and reconstructs its complete message log.
func (s *Store) Resume(id string) (*Session, error) {
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
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat session %q: %w", id, err)
	}

	messages, err := readMessages(file, id)
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
		store:     s,
	}, nil
}

func readMessages(reader io.Reader, id string) ([]Message, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	messages := make([]Message, 0)
	line := 0
	for scanner.Scan() {
		line++
		data := strings.TrimSpace(scanner.Text())
		if data == "" {
			continue
		}
		var message Message
		if err := json.Unmarshal([]byte(data), &message); err != nil {
			return nil, fmt.Errorf("decode session %q record %d: %w", id, line, err)
		}
		if err := validateMessage(message); err != nil {
			return nil, fmt.Errorf("validate session %q record %d: %w", id, line, err)
		}
		messages = append(messages, message)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read session %q: %w", id, err)
	}
	return messages, nil
}

func validateMessage(message Message) error {
	if message.Role != RoleUser && message.Role != RoleAssistant {
		return fmt.Errorf("session message role must be %q or %q, got %q", RoleUser, RoleAssistant, message.Role)
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

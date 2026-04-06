package auth

import (
	"context"
	"encoding/base64"
	"sync"

	"github.com/go-faster/errors"
	"github.com/gotd/td/session"
)

// PersistentSessionStorage wraps a base session.Storage and persists updates to disk
// This ensures that session tokens are automatically saved when they're updated by the Telegram client.
//
// Usage:
//
//	baseStorage := &session.StorageMemory{}
//	sessionData := &SessionData{...}
//	persistentStorage := NewPersistentSessionStorage(baseStorage, sessionPath, sessionData)
//	// Use persistentStorage with telegram.Client
//	client, err := telegram.NewClient(..., persistentStorage)
//	// Any token updates via client.Run() will auto-save to disk
//
// This is critical because the gotd/td library updates session tokens during:
// - Initial authentication
// - Reconnection events
// - Other long-running operations
//
// Without this wrapper, session tokens become stale on app restart and authentication fails.
// This matches the pattern used in teldrive's PostgreSQL session storage.
type PersistentSessionStorage struct {
	mu          sync.RWMutex
	base        session.Storage
	sessionPath string
	sessionData *SessionData
}

// NewPersistentSessionStorage creates a new storage that persists session updates to disk
func NewPersistentSessionStorage(base session.Storage, sessionPath string, sessionData *SessionData) *PersistentSessionStorage {
	return &PersistentSessionStorage{
		base:        base,
		sessionPath: sessionPath,
		sessionData: sessionData,
	}
}

// LoadSession loads the session from base storage
func (s *PersistentSessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	return s.base.LoadSession(ctx)
}

// StoreSession stores the session in base storage AND updates our persisted session.json
// This is critical because gotd/td updates session tokens during operation.
// Without this, the session token becomes stale on app restart.
//
// Implementation note: Uses mutex locking to ensure atomic updates to disk.
func (s *PersistentSessionStorage) StoreSession(ctx context.Context, data []byte) error {
	// First, store in base storage
	if err := s.base.StoreSession(ctx, data); err != nil {
		return errors.Wrap(err, "store in base storage")
	}

	// Then, update our persisted session file with the new token
	s.mu.Lock()
	defer s.mu.Unlock()

	// Encode the new session data as base64
	tgSessionB64 := base64.StdEncoding.EncodeToString(data)
	s.sessionData.TelegramSession = tgSessionB64

	// Save to disk
	if err := SaveSession(s.sessionPath, s.sessionData); err != nil {
		return errors.Wrap(err, "save session to disk")
	}

	return nil
}

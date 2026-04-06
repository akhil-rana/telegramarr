package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// LoadSession loads session metadata from session.json
func LoadSession(sessionPath string) (*SessionData, error) {
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var session SessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	return &session, nil
}

// SaveSession saves session metadata to session.json
func SaveSession(sessionPath string, session *SessionData) error {
	// Only update LastAuthenticated if not already set
	if session.LastAuthenticated.IsZero() {
		session.LastAuthenticated = time.Now()
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	if err := os.WriteFile(sessionPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	return nil
}

// SessionExists checks if session files exist
func SessionExists(sessionPath string) bool {
	_, err := os.Stat(sessionPath)
	return err == nil
}

// DeleteSession removes session files
func DeleteSession(sessionPath, sessionDBPath string) error {
	if err := os.Remove(sessionPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete session file: %w", err)
	}

	if err := os.Remove(sessionDBPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete session db: %w", err)
	}

	return nil
}

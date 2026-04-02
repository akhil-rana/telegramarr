package auth

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"go.uber.org/zap"
)

// CreateClientFromSession creates a Telegram client from a stored session
// This allows reusing an authenticated session without going through auth flow again
// WARNING: The client MUST be run in a goroutine like: go client.Run(ctx, ...)
func CreateClientFromSession(ctx context.Context, appID int, appHash string, sessionData *SessionData, logger *zap.Logger) (*telegram.Client, error) {
	if sessionData == nil || sessionData.TelegramSession == "" {
		logger.Error("No session data available")
		return nil, fmt.Errorf("no session data available")
	}

	// Decode the base64 session
	sessionBytes, err := base64.StdEncoding.DecodeString(sessionData.TelegramSession)
	if err != nil {
		logger.Error("Failed to decode session", zap.Error(err))
		return nil, err
	}

	logger.Info("Session decoded", zap.Int("bytes", len(sessionBytes)))

	// Create in-memory storage with the decoded session
	memStorage := &session.StorageMemory{}
	if err := memStorage.StoreSession(ctx, sessionBytes); err != nil {
		logger.Error("Failed to store session in memory", zap.Error(err))
		return nil, err
	}

	logger.Info("Session stored in memory")

	// Create the telegram client with the stored session
	client := telegram.NewClient(
		appID,
		appHash,
		telegram.Options{
			SessionStorage: memStorage,
		},
	)

	logger.Info("Telegram client created")

	return client, nil
}

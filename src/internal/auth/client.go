package auth

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"go.uber.org/zap"

	"github.com/akhil-rana/telegramarr/internal/config"
	tgc "github.com/akhil-rana/telegramarr/internal/telegram"
)

// CreateClientFromSession creates a Telegram client from a stored session
// This allows reusing an authenticated session without going through auth flow again
// Uses middleware chain for proper handling of FLOOD_WAIT and rate limits
// WARNING: The client MUST be run in a goroutine like: go client.Run(ctx, ...)
func CreateClientFromSession(ctx context.Context, appID int, appHash string, sessionData *SessionData, cfg *config.TelegramConfig, logger *zap.Logger) (*telegram.Client, error) {
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

	// Create middlewares for the client
	// Order: FloodWait -> Recovery -> Retry -> RateLimit
	// FloodWait handles FLOOD_WAIT transparently
	// Recovery provides exponential backoff on transient errors
	// Retry immediately retries on specific Telegram errors
	// RateLimit applies global rate limiting if enabled in config (default: disabled)
	// Use background context for recovery to avoid cancellation during RPC calls
	bgCtx := context.Background()
	middlewares := tgc.NewMiddleware(cfg,
		tgc.WithFloodWait(),
		tgc.WithRecovery(bgCtx),
		tgc.WithRetry(cfg.MaxRetries),
		tgc.WithRateLimit(),
	)

	// Create the telegram client with the stored session and middlewares
	client := telegram.NewClient(
		appID,
		appHash,
		telegram.Options{
			SessionStorage: memStorage,
			Middlewares:    middlewares,
			Device: telegram.DeviceConfig{
				DeviceModel:    cfg.DeviceModel,
				SystemVersion:  cfg.SystemVersion,
				AppVersion:     cfg.AppVersion,
				SystemLangCode: cfg.SystemLangCode,
				LangPack:       cfg.LangPack,
				LangCode:       cfg.LangCode,
			},
		},
	)

	logger.Info("Telegram client created with middleware chain")

	return client, nil
}

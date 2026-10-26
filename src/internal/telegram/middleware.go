package telegram

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/td/telegram"
	"golang.org/x/time/rate"

	"github.com/akhil-rana/telegramarr/internal/config"
	"github.com/akhil-rana/telegramarr/internal/recovery"
	"github.com/akhil-rana/telegramarr/internal/retry"
)

type middlewareConfig struct {
	config      *config.TelegramConfig
	middlewares []telegram.Middleware
}

type middlewareOption func(*middlewareConfig)

// NewMiddleware creates a middleware chain for Telegram client operations
// Options control what middlewares are included in the chain
func NewMiddleware(cfg *config.TelegramConfig, opts ...middlewareOption) []telegram.Middleware {
	mc := &middlewareConfig{
		config:      cfg,
		middlewares: []telegram.Middleware{},
	}
	for _, opt := range opts {
		opt(mc)
	}
	return mc.middlewares
}

// WithFloodWait adds flood wait handling middleware
// Automatically waits when Telegram sends FLOOD_WAIT without blocking other threads
func WithFloodWait() middlewareOption {
	return func(mc *middlewareConfig) {
		mc.middlewares = append(mc.middlewares, floodwait.NewSimpleWaiter())
	}
}

// WithRecovery adds recovery middleware with exponential backoff
// Retries failed operations with exponential backoff strategy
func WithRecovery(ctx context.Context) middlewareOption {
	return func(mc *middlewareConfig) {
		mc.middlewares = append(mc.middlewares,
			recovery.New(ctx, newBackoff(10*time.Second)))
	}
}

// WithRetry adds retry middleware for specific Telegram errors
// Immediately retries on recoverable Telegram errors (Timedout, RPC_CALL_FAIL, WORKER_BUSY, etc.)
func WithRetry(maxRetries int) middlewareOption {
	return func(mc *middlewareConfig) {
		mc.middlewares = append(mc.middlewares, retry.New(maxRetries))
	}
}

// WithRateLimit adds global rate limiting if enabled in config
// Rate limit is applied per-request (default 100ms) with burst capacity (default 5)
func WithRateLimit() middlewareOption {
	return func(mc *middlewareConfig) {
		if mc.config.RateLimit {
			mc.middlewares = append(mc.middlewares,
				ratelimit.New(
					rate.Every(time.Millisecond*time.Duration(mc.config.Rate)),
					mc.config.RateBurst,
				))
		}
	}
}

// newBackoff creates an exponential backoff strategy for recovery
func newBackoff(timeout time.Duration) backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.Multiplier = 1.1
	b.MaxElapsedTime = timeout
	b.MaxInterval = 10 * time.Second
	return b
}

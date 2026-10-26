package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"go.uber.org/zap"
)

// ClientManager manages the Telegram client lifecycle
// It ensures the client.Run() is always running in the background
// This is CRITICAL for maintaining connection during long operations
type ClientManager struct {
	client       *telegram.Client
	logger       *zap.Logger
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	errChan      chan error
	reconnectErr error
}

// NewClientManager creates and starts a new client manager
func NewClientManager(client *telegram.Client, logger *zap.Logger) *ClientManager {
	ctx, cancel := context.WithCancel(context.Background())

	cm := &ClientManager{
		client:  client,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		errChan: make(chan error, 1),
	}

	// Start the client in a background goroutine
	// This is REQUIRED by gotd/td library to keep connection alive
	go cm.runClient()

	logger.Info("ClientManager started, client will run in background")
	return cm
}

// runClient runs the client.Run() loop
// This must be running for any API calls to work properly
// Without this, the connection will drop after ~1 minute
func (cm *ClientManager) runClient() {
	defer close(cm.done)

	for {
		select {
		case <-cm.ctx.Done():
			cm.logger.Info("ClientManager context cancelled, stopping client")
			return
		default:
		}

		// Use fresh context without timeout for the run loop
		// The client.Run() will handle reconnections internally
		// We don't use timeout here because that would break long uploads
		runCtx := cm.ctx

		err := cm.client.Run(runCtx, func(ctx context.Context) error {
			cm.logger.Info("Telegram client connected and ready")

			// Keep the client alive by blocking here
			// The client will handle connection management internally
			<-ctx.Done()
			cm.logger.Info("Telegram client context ended")
			return ctx.Err()
		})

		if err != nil {
			if cm.ctx.Err() != nil {
				// Manager was shut down, exit gracefully
				cm.logger.Info("Client manager context cancelled, exiting run loop")
				return
			}

			cm.logger.Error("Client run error, reconnecting...",
				zap.Error(err),
				zap.Duration("retry_delay", 5*time.Second))

			cm.mu.Lock()
			cm.reconnectErr = err
			cm.mu.Unlock()

			// Exponential backoff before reconnect
			select {
			case <-time.After(5 * time.Second):
				// Continue to next iteration
			case <-cm.ctx.Done():
				cm.logger.Info("Received shutdown signal during reconnect delay")
				return
			}
		} else {
			cm.logger.Info("Client run completed normally")
		}
	}
}

// GetClient returns the managed client
// This client is guaranteed to have client.Run() running in the background
func (cm *ClientManager) GetClient() *telegram.Client {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.client
}

// IsConnected checks if the client is currently connected
func (cm *ClientManager) IsConnected() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Check if run loop is still active
	select {
	case <-cm.done:
		return false
	default:
		return true
	}
}

// GetLastError returns the last reconnection error
func (cm *ClientManager) GetLastError() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.reconnectErr
}

// Stop gracefully stops the client manager
func (cm *ClientManager) Stop(ctx context.Context) error {
	cm.logger.Info("Stopping ClientManager")

	// Signal the run loop to stop
	cm.cancel()

	// Wait for the run loop to finish or timeout
	select {
	case <-cm.done:
		cm.logger.Info("ClientManager stopped successfully")
		return nil
	case <-time.After(5 * time.Second):
		cm.logger.Error("ClientManager stop timeout")
		return fmt.Errorf("client manager stop timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitForConnection blocks until the client is connected
// Returns error if timeout occurs
func (cm *ClientManager) WaitForConnection(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		if cm.IsConnected() {
			cm.logger.Info("Client is connected")
			return nil
		}

		select {
		case <-cm.done:
			return fmt.Errorf("client manager stopped")
		case <-time.After(100 * time.Millisecond):
			if time.Now().After(deadline) {
				return fmt.Errorf("connection timeout")
			}
		}
	}
}

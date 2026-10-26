// Pool implementation for connection pooling
// Based on teldrive's pool implementation
package pool

import (
	"context"
	"sync"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// Pool represents a pool of Telegram client connections
type Pool interface {
	Client(ctx context.Context, dc int) *tg.Client
	Default(ctx context.Context) *tg.Client
	Close() error
}

type pool struct {
	api         *telegram.Client
	size        int64
	mu          *sync.Mutex
	middlewares []telegram.Middleware
	invoke      tg.Invoker
	close       func() error
	logger      *zap.Logger
}

func chainMiddlewares(invoker tg.Invoker, chain ...telegram.Middleware) tg.Invoker {
	if len(chain) == 0 {
		return invoker
	}
	for i := len(chain) - 1; i >= 0; i-- {
		invoker = chain[i].Handle(invoker)
	}

	return invoker
}

// NewPool creates a new connection pool for the Telegram client
func NewPool(c *telegram.Client, size int64, logger *zap.Logger, middlewares ...telegram.Middleware) Pool {
	if size < 1 {
		size = 1
	}
	return &pool{
		api:         c,
		size:        size,
		mu:          &sync.Mutex{},
		middlewares: middlewares,
		logger:      logger,
	}
}

func (p *pool) current() int {
	return p.api.Config().ThisDC
}

// Client returns a pooled Telegram client for the specified datacenter
func (p *pool) Client(ctx context.Context, dc int) *tg.Client {
	return tg.NewClient(p.invoker(ctx, dc))
}

func (p *pool) invoker(ctx context.Context, dc int) tg.Invoker {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.invoke != nil {
		return p.invoke
	}

	var (
		invoker telegram.CloseInvoker
		err     error
	)
	if dc == p.current() {
		invoker, err = p.api.Pool(p.size)
	} else {
		invoker, err = p.api.DC(ctx, dc, p.size)
	}

	if err != nil {
		p.logger.Error("Failed to create pool invoker", zap.Error(err), zap.Int64("size", p.size))
		return p.api
	}

	p.close = invoker.Close
	p.invoke = chainMiddlewares(invoker, p.middlewares...)

	return p.invoke
}

// Default returns a pooled Telegram client for the current datacenter
func (p *pool) Default(ctx context.Context) *tg.Client {
	return p.Client(ctx, p.current())
}

// Close closes the connection pool
func (p *pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.close != nil {
		err := p.close()
		p.close = nil
		return err
	}
	return nil
}

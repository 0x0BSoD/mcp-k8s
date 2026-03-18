package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/0x0BSoD/mcp-k8s/pkg/correlation"
	"github.com/0x0BSoD/mcp-k8s/pkg/planner"
)

type Coordinator struct {
	cfg Config
}

func New() (Coordinator, error) {
	return Coordinator{cfg: fromEnv()}, nil
}

func (c *Coordinator) Run(ctx context.Context) error {
	slog.Info("coordinator starting", "http_addr", c.cfg.HTTPAddr)

	registry := NewRegistry()
	executor := NewExecutor(registry)
	p := planner.New()
	corr := correlation.New()
	builder := NewResponseBuilder()

	gw := newGateway(registry, executor, p, corr, builder)

	srv := &http.Server{
		Addr:    c.cfg.HTTPAddr,
		Handler: gw.routes(),
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()

	slog.Info("coordinator ready", "addr", c.cfg.HTTPAddr)

	select {
	case <-ctx.Done():
		slog.Info("shutting down coordinator")
		if err := srv.Shutdown(context.Background()); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
		return nil
	case err := <-serveErr:
		return fmt.Errorf("http serve: %w", err)
	}
}

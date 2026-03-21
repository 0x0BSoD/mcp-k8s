package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/0x0BSoD/mcp-k8s/pkg/correlation"
	"github.com/0x0BSoD/mcp-k8s/pkg/llm"
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

	registry := NewRegistry(c.cfg.RegistryFile)
	if err := registry.Load(); err != nil {
		slog.Warn("failed to load registry", "err", err)
	}
	executor := NewExecutor(registry)
	p := planner.New()
	corr := correlation.New()
	builder := NewResponseBuilder()

	var llmClient llm.Client
	if c.cfg.OllamaBaseURL != "" {
		llmClient = llm.NewOllama(c.cfg.OllamaBaseURL, c.cfg.OllamaModel)
		slog.Info("LLM enabled", "base_url", c.cfg.OllamaBaseURL, "model", c.cfg.OllamaModel)
	} else {
		slog.Info("LLM disabled (OLLAMA_BASE_URL not set)")
	}

	gw := newGateway(registry, executor, p, corr, builder, c.cfg.RegistryToken, llmClient)

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

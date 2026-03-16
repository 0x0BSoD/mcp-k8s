package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/0x0BSoD/mcp-k8s/internal/app/coordinator"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx := context.Background()

	app, err := coordinator.New()
	if err != nil {
		slog.Error("create coordinator err:", err)
		os.Exit(1)
	}

	if err := app.Run(ctx); err != nil {
		slog.Error("run err:", err)
		os.Exit(1)
	}
}

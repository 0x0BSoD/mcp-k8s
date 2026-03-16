package coordinator

import (
	"context"
	"log/slog"
)

type Coordinator struct {
}

func New() (Coordinator, error) {
	return Coordinator{}, nil
}

func (c *Coordinator) Run(ctx context.Context) error {
	slog.Info("coordinator start")

	return nil
}

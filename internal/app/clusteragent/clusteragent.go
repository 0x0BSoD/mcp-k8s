package clusteragent

import (
	"context"
	"log/slog"
)

type ClusterAgent struct {
}

func New() (ClusterAgent, error) {
	return ClusterAgent{}, nil
}

func (c *ClusterAgent) Run(ctx context.Context) error {
	slog.Info("cluster agent start")

	return nil
}

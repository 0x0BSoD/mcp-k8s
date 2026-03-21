package clusteragent

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/0x0BSoD/mcp-k8s/pkg/enrichment"
	"github.com/0x0BSoD/mcp-k8s/pkg/store"
	pb "github.com/0x0BSoD/mcp-k8s/proto/gen/clusteragentpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ClusterAgent is the top-level application struct for the in-cluster agent.
type ClusterAgent struct {
	cfg Config
}

func New() (ClusterAgent, error) {
	cfg, err := FromEnv()
	if err != nil {
		return ClusterAgent{}, fmt.Errorf("config: %w", err)
	}
	return ClusterAgent{cfg: cfg}, nil
}

func (a *ClusterAgent) Run(ctx context.Context) error {
	slog.Info("cluster agent starting", "cluster", a.cfg.ClusterName, "grpc_addr", a.cfg.GRPCAddr)

	// ── Kubernetes client ──────────────────────────────────────────────────
	restCfg, err := a.cfg.KubeConfig()
	if err != nil {
		return fmt.Errorf("kubeconfig: %w", err)
	}
	k8sClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("k8s client: %w", err)
	}

	metricsClient, err := metricsv1beta1.NewForConfig(restCfg)
	if err != nil {
		// metrics-server may not be installed; log and continue with nil client
		slog.Warn("metrics client unavailable, pod metrics disabled", "err", err)
		metricsClient = nil
	}

	// ── Core components ────────────────────────────────────────────────────
	eventStore := store.New(a.cfg.StoreMaxSize)
	enricher := enrichment.New(k8sClient)

	factory := informers.NewSharedInformerFactoryWithOptions(
		k8sClient,
		0, // no periodic resync — we rely on watch updates
		informers.WithNamespace(a.cfg.WatchNamespace),
	)

	w := newWatcher(a.cfg.ClusterName, enricher, eventStore, factory)
	if err := w.Start(ctx); err != nil {
		return fmt.Errorf("watcher start: %w", err)
	}

	// ── gRPC server ────────────────────────────────────────────────────────
	lis, err := net.Listen("tcp", a.cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", a.cfg.GRPCAddr, err)
	}

	srv := grpc.NewServer()
	pb.RegisterClusterAgentServiceServer(srv, newGRPCServer(eventStore, enricher, k8sClient, metricsClient))

	// Standard gRPC health protocol — required for Kubernetes gRPC probes.
	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(srv, healthSrv)
	healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	slog.Info("gRPC server listening", "addr", a.cfg.GRPCAddr)

	// Serve in a goroutine; shut down when ctx is cancelled.
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(lis)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		srv.GracefulStop()
		return nil
	case err := <-serveErr:
		return fmt.Errorf("grpc serve: %w", err)
	}
}

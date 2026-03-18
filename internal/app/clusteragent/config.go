package clusteragent

import (
	"fmt"
	"os"
	"strconv"

	"github.com/0x0BSoD/mcp-k8s/pkg/store"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Config holds all runtime configuration for the cluster agent.
type Config struct {
	ClusterName    string
	GRPCAddr       string
	WatchNamespace string // empty = all namespaces
	StoreMaxSize   int
	KubeconfigPath string
}

// FromEnv reads configuration from environment variables.
// MCP_CLUSTER_NAME is required; all others have defaults.
func FromEnv() (Config, error) {
	cfg := Config{
		ClusterName:    os.Getenv("MCP_CLUSTER_NAME"),
		GRPCAddr:       envOrDefault("MCP_GRPC_ADDR", ":50051"),
		WatchNamespace: os.Getenv("MCP_WATCH_NAMESPACE"),
		KubeconfigPath: os.Getenv("KUBECONFIG"),
		StoreMaxSize:   envIntOrDefault("MCP_STORE_MAX_SIZE", store.DefaultMaxSize),
	}
	if cfg.ClusterName == "" {
		return Config{}, fmt.Errorf("MCP_CLUSTER_NAME is required")
	}
	return cfg, nil
}

// KubeConfig builds a *rest.Config, preferring in-cluster config and
// falling back to the kubeconfig file path from the environment.
func (c Config) KubeConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	path := c.KubeconfigPath
	if path == "" {
		path = clientcmd.RecommendedHomeFile
	}
	return clientcmd.BuildConfigFromFlags("", path)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

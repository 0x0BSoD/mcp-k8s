package coordinator

import (
	"fmt"
	"sync"

	pb "github.com/0x0BSoD/mcp-k8s/proto/gen/clusteragentpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ClusterEntry holds metadata and a lazily-created gRPC connection for one
// cluster agent.
type ClusterEntry struct {
	ClusterName  string
	GRPCEndpoint string
	Labels       map[string]string
}

// Registry is an in-memory store of cluster agents.
// Connections are created on first use and reused afterwards.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]ClusterEntry
	conns   map[string]*grpc.ClientConn
}

func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string]ClusterEntry),
		conns:   make(map[string]*grpc.ClientConn),
	}
}

// Register adds or replaces a cluster entry.
// If a connection already exists for this cluster it is closed first.
func (r *Registry) Register(entry ClusterEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if conn, ok := r.conns[entry.ClusterName]; ok {
		_ = conn.Close()
		delete(r.conns, entry.ClusterName)
	}
	r.entries[entry.ClusterName] = entry
}

// Unregister removes a cluster and closes its connection.
func (r *Registry) Unregister(clusterName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if conn, ok := r.conns[clusterName]; ok {
		_ = conn.Close()
		delete(r.conns, clusterName)
	}
	delete(r.entries, clusterName)
}

// List returns all registered cluster entries.
func (r *Registry) List() []ClusterEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ClusterEntry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	return out
}

// Client returns a gRPC client for the named cluster, creating the connection
// if it does not already exist.
func (r *Registry) Client(clusterName string) (pb.ClusterAgentServiceClient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if conn, ok := r.conns[clusterName]; ok {
		return pb.NewClusterAgentServiceClient(conn), nil
	}

	entry, ok := r.entries[clusterName]
	if !ok {
		return nil, fmt.Errorf("cluster %q not registered", clusterName)
	}

	conn, err := grpc.NewClient(
		entry.GRPCEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", entry.GRPCEndpoint, err)
	}
	r.conns[clusterName] = conn
	return pb.NewClusterAgentServiceClient(conn), nil
}

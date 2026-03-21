package coordinator

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	pb "github.com/0x0BSoD/mcp-k8s/proto/gen/clusteragentpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ClusterEntry holds metadata and a lazily-created gRPC connection for one
// cluster agent.
type ClusterEntry struct {
	ClusterName  string            `json:"cluster_name"`
	GRPCEndpoint string            `json:"grpc_endpoint"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// Registry is a store of cluster agents backed by an optional JSON file.
// Connections are created on first use and reused afterwards.
type Registry struct {
	mu       sync.RWMutex
	entries  map[string]ClusterEntry
	conns    map[string]*grpc.ClientConn
	filePath string // empty = in-memory only
}

func NewRegistry(filePath string) *Registry {
	return &Registry{
		entries:  make(map[string]ClusterEntry),
		conns:    make(map[string]*grpc.ClientConn),
		filePath: filePath,
	}
}

// Load reads persisted registrations from the registry file.
// Safe to call when the file does not exist yet.
func (r *Registry) Load() error {
	if r.filePath == "" {
		return nil
	}
	data, err := os.ReadFile(r.filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read registry file %s: %w", r.filePath, err)
	}

	var entries []ClusterEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse registry file %s: %w", r.filePath, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range entries {
		r.entries[e.ClusterName] = e
	}
	slog.Info("registry loaded", "clusters", len(entries), "file", r.filePath)
	return nil
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
	r.save()
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
	r.save()
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

// save writes current entries to the registry file atomically (temp + rename).
// Must be called with r.mu held for writing.
func (r *Registry) save() {
	if r.filePath == "" {
		return
	}

	entries := make([]ClusterEntry, 0, len(r.entries))
	for _, e := range r.entries {
		entries = append(entries, e)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		slog.Error("registry save: marshal failed", "err", err)
		return
	}

	dir := filepath.Dir(r.filePath)
	tmp, err := os.CreateTemp(dir, ".registry-*.tmp")
	if err != nil {
		slog.Error("registry save: create temp file failed", "err", err)
		return
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		slog.Error("registry save: write failed", "err", err)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		slog.Error("registry save: close failed", "err", err)
		return
	}
	if err := os.Rename(tmpName, r.filePath); err != nil {
		_ = os.Remove(tmpName)
		slog.Error("registry save: rename failed", "err", err)
		return
	}
}

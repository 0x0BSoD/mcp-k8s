package clusteragent

import (
	"context"
	"time"

	"github.com/0x0BSoD/mcp-k8s/pkg/enrichment"
	"github.com/0x0BSoD/mcp-k8s/pkg/events"
	"github.com/0x0BSoD/mcp-k8s/pkg/store"
	pb "github.com/0x0BSoD/mcp-k8s/proto/gen/clusteragentpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// grpcServer implements pb.ClusterAgentServiceServer.
type grpcServer struct {
	pb.UnimplementedClusterAgentServiceServer
	store    *store.Store
	enricher *enrichment.Enricher
}

func newGRPCServer(s *store.Store, e *enrichment.Enricher) *grpcServer {
	return &grpcServer{store: s, enricher: e}
}

// ── ListEvents ────────────────────────────────────────────────────────────────

func (s *grpcServer) ListEvents(_ context.Context, req *pb.ListEventsRequest) (*pb.ListEventsResponse, error) {
	f := store.Filter{
		Namespace: req.Namespace,
		Reason:    req.Reason,
		Limit:     int(req.Limit),
	}
	if req.Since != nil {
		f.Since = req.Since.AsTime()
	}

	evs := s.store.Query(f)
	resp := &pb.ListEventsResponse{
		Events: make([]*pb.NormalizedEvent, len(evs)),
	}
	for i, e := range evs {
		resp.Events[i] = toProtoEvent(e)
	}
	return resp, nil
}

// ── GetEventsForObject ────────────────────────────────────────────────────────

func (s *grpcServer) GetEventsForObject(_ context.Context, req *pb.GetEventsForObjectRequest) (*pb.GetEventsForObjectResponse, error) {
	// Query by namespace; filter by kind+name in memory (no name index in store).
	candidates := s.store.Query(store.Filter{Namespace: req.Namespace})

	var matched []*pb.NormalizedEvent
	for _, e := range candidates {
		if e.InvolvedObjectKind == req.Kind && e.InvolvedObjectName == req.Name {
			matched = append(matched, toProtoEvent(e))
		}
	}
	return &pb.GetEventsForObjectResponse{Events: matched}, nil
}

// ── GetNamespaceSummary ───────────────────────────────────────────────────────

func (s *grpcServer) GetNamespaceSummary(_ context.Context, req *pb.GetNamespaceSummaryRequest) (*pb.GetNamespaceSummaryResponse, error) {
	var since time.Time
	if req.Since != nil {
		since = req.Since.AsTime()
	}

	groups := s.store.SummarizeNamespace(req.Namespace, since)

	protoGroups := make([]*pb.NamespaceEventGroup, len(groups))
	for i, g := range groups {
		protoGroups[i] = &pb.NamespaceEventGroup{
			Reason:     g.Reason,
			TotalCount: g.TotalCount,
			Objects:    g.Objects,
			LastSeen:   timestamppb.New(g.LastSeen),
		}
	}
	return &pb.GetNamespaceSummaryResponse{
		Namespace: req.Namespace,
		Groups:    protoGroups,
	}, nil
}

// ── ResolveOwnerChain ─────────────────────────────────────────────────────────

func (s *grpcServer) ResolveOwnerChain(ctx context.Context, req *pb.ResolveOwnerChainRequest) (*pb.ResolveOwnerChainResponse, error) {
	chain, err := s.enricher.ResolveOwnerChain(ctx, req.Namespace, req.Kind, req.Name)
	if err != nil {
		return nil, err
	}
	return &pb.ResolveOwnerChainResponse{Chain: toProtoOwnerChain(chain)}, nil
}

// ── GetObjectSnapshot ─────────────────────────────────────────────────────────

func (s *grpcServer) GetObjectSnapshot(ctx context.Context, req *pb.GetObjectSnapshotRequest) (*pb.GetObjectSnapshotResponse, error) {
	snap, err := s.enricher.GetObjectSnapshot(ctx, req.Namespace, req.Kind, req.Name)
	if err != nil {
		return nil, err
	}
	return &pb.GetObjectSnapshotResponse{
		Snapshot: &pb.ObjectSnapshot{
			Namespace:     snap.Namespace,
			Kind:          snap.Kind,
			Name:          snap.Name,
			Uid:           snap.UID,
			Phase:         snap.Phase,
			Labels:        snap.Labels,
			Annotations:   snap.Annotations,
			OwnerChain:    toProtoOwnerChain(snap.OwnerChain),
			RawStatusJson: snap.RawStatusJSON,
		},
	}, nil
}

// ── Conversions ───────────────────────────────────────────────────────────────

func toProtoEvent(e events.NormalizedEvent) *pb.NormalizedEvent {
	return &pb.NormalizedEvent{
		ClusterName:        e.ClusterName,
		Namespace:          e.Namespace,
		InvolvedObjectKind: e.InvolvedObjectKind,
		InvolvedObjectName: e.InvolvedObjectName,
		InvolvedObjectUid:  e.InvolvedObjectUID,
		OwnerChain:         toProtoOwnerChain(e.OwnerChain),
		Reason:             e.Reason,
		Message:            e.Message,
		Count:              e.Count,
		FirstSeen:          timestamppb.New(e.FirstSeen),
		LastSeen:           timestamppb.New(e.LastSeen),
	}
}

func toProtoOwnerChain(chain []events.OwnerRef) []*pb.OwnerRef {
	if len(chain) == 0 {
		return nil
	}
	out := make([]*pb.OwnerRef, len(chain))
	for i, r := range chain {
		out[i] = &pb.OwnerRef{Kind: r.Kind, Name: r.Name, Uid: r.UID}
	}
	return out
}

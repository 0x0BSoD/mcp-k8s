package clusteragent

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/0x0BSoD/mcp-k8s/pkg/enrichment"
	"github.com/0x0BSoD/mcp-k8s/pkg/events"
	"github.com/0x0BSoD/mcp-k8s/pkg/store"
	pb "github.com/0x0BSoD/mcp-k8s/proto/gen/clusteragentpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsapi "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"
)

const defaultLogTailLines = 50

// grpcServer implements pb.ClusterAgentServiceServer.
type grpcServer struct {
	pb.UnimplementedClusterAgentServiceServer
	store    *store.Store
	enricher *enrichment.Enricher
	k8s      kubernetes.Interface
	metrics  metricsv1beta1.Interface
}

func newGRPCServer(s *store.Store, e *enrichment.Enricher, k kubernetes.Interface, m metricsv1beta1.Interface) *grpcServer {
	return &grpcServer{store: s, enricher: e, k8s: k, metrics: m}
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
			Namespace:          snap.Namespace,
			Kind:               snap.Kind,
			Name:               snap.Name,
			Uid:                snap.UID,
			Phase:              snap.Phase,
			Labels:             snap.Labels,
			Annotations:        snap.Annotations,
			OwnerChain:         toProtoOwnerChain(snap.OwnerChain),
			RawStatusJson:      snap.RawStatusJSON,
			ContainerStatuses:  toProtoContainerStatuses(snap.ContainerStatuses),
			WorkloadConditions: toProtoWorkloadConditions(snap.WorkloadConditions),
		},
	}, nil
}

// ── GetContainerLogs ──────────────────────────────────────────────────────────

func (s *grpcServer) GetContainerLogs(ctx context.Context, req *pb.GetContainerLogsRequest) (*pb.GetContainerLogsResponse, error) {
	tailLines := int64(req.TailLines)
	if tailLines <= 0 {
		tailLines = defaultLogTailLines
	}

	opts := &corev1.PodLogOptions{
		Container: req.Container,
		TailLines: &tailLines,
	}

	rc, err := s.k8s.CoreV1().Pods(req.Namespace).GetLogs(req.Pod, opts).Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rc); err != nil {
		return nil, err
	}

	return &pb.GetContainerLogsResponse{
		Pod:       req.Pod,
		Container: req.Container,
		Logs:      buf.String(),
	}, nil
}

// ── GetPodMetrics ─────────────────────────────────────────────────────────────

func (s *grpcServer) GetPodMetrics(ctx context.Context, req *pb.GetPodMetricsRequest) (*pb.GetPodMetricsResponse, error) {
	if s.metrics == nil {
		return &pb.GetPodMetricsResponse{}, nil
	}

	var pbPods []*pb.PodMetrics

	if req.Pod != "" {
		// Single pod.
		m, err := s.metrics.MetricsV1beta1().PodMetricses(req.Namespace).Get(ctx, req.Pod, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		pbPods = append(pbPods, podMetricsToPB(m.Namespace, m.Name, toPBContainerMetrics(m.Containers), timestamppb.New(m.Timestamp.Time)))
	} else {
		// All pods in namespace (empty namespace = all namespaces).
		list, err := s.metrics.MetricsV1beta1().PodMetricses(req.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, m := range list.Items {
			pbPods = append(pbPods, podMetricsToPB(m.Namespace, m.Name, toPBContainerMetrics(m.Containers), timestamppb.New(m.Timestamp.Time)))
		}
	}

	return &pb.GetPodMetricsResponse{Pods: pbPods}, nil
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

func toProtoContainerStatuses(statuses []enrichment.ContainerStatus) []*pb.ContainerStatus {
	if len(statuses) == 0 {
		return nil
	}
	out := make([]*pb.ContainerStatus, len(statuses))
	for i, s := range statuses {
		out[i] = &pb.ContainerStatus{
			Name:                    s.Name,
			Ready:                   s.Ready,
			RestartCount:            s.RestartCount,
			State:                   s.State,
			Reason:                  s.Reason,
			LastTerminationExitCode: s.LastTerminationExitCode,
			LastTerminationReason:   s.LastTerminationReason,
			LastTerminationMessage:  s.LastTerminationMessage,
		}
	}
	return out
}

func toProtoWorkloadConditions(conditions []enrichment.WorkloadCondition) []*pb.WorkloadCondition {
	if len(conditions) == 0 {
		return nil
	}
	out := make([]*pb.WorkloadCondition, len(conditions))
	for i, c := range conditions {
		out[i] = &pb.WorkloadCondition{
			Type:    c.Type,
			Status:  c.Status,
			Reason:  c.Reason,
			Message: c.Message,
		}
	}
	return out
}

func toProtoSnapshot(snap *enrichment.ObjectSnapshot) *pb.ObjectSnapshot {
	return &pb.ObjectSnapshot{
		Namespace:          snap.Namespace,
		Kind:               snap.Kind,
		Name:               snap.Name,
		Uid:                snap.UID,
		Phase:              snap.Phase,
		Labels:             snap.Labels,
		Annotations:        snap.Annotations,
		OwnerChain:         toProtoOwnerChain(snap.OwnerChain),
		RawStatusJson:      snap.RawStatusJSON,
		ContainerStatuses:  toProtoContainerStatuses(snap.ContainerStatuses),
		WorkloadConditions: toProtoWorkloadConditions(snap.WorkloadConditions),
	}
}

func toProtoSnapshotPtr(snap *enrichment.ObjectSnapshot) *pb.ObjectSnapshot {
	if snap == nil {
		return nil
	}
	return toProtoSnapshot(snap)
}

// ensure toProtoSnapshotPtr is used (avoids dead-code removal).
var _ = toProtoSnapshotPtr

func toPBContainerMetrics(containers []metricsapi.ContainerMetrics) []*pb.ContainerMetrics {
	out := make([]*pb.ContainerMetrics, len(containers))
	for i, c := range containers {
		out[i] = &pb.ContainerMetrics{
			Name:          c.Name,
			CpuMillicores: c.Usage.Cpu().MilliValue(),
			MemoryBytes:   c.Usage.Memory().Value(),
		}
	}
	return out
}

func podMetricsToPB(namespace, pod string, containers []*pb.ContainerMetrics, ts *timestamppb.Timestamp) *pb.PodMetrics {
	return &pb.PodMetrics{
		Namespace:  namespace,
		Pod:        pod,
		Containers: containers,
		Timestamp:  ts,
	}
}

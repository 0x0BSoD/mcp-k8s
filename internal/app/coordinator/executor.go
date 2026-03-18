package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/0x0BSoD/mcp-k8s/pkg/events"
	"github.com/0x0BSoD/mcp-k8s/pkg/planner"
	pb "github.com/0x0BSoD/mcp-k8s/proto/gen/clusteragentpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ExecutionResult aggregates everything collected by executing a Plan.
type ExecutionResult struct {
	Events             []events.NormalizedEvent
	Snapshots          []*pb.ObjectSnapshot
	Chains             [][]*pb.OwnerRef
	NamespaceSummaries []*pb.NamespaceEventGroup
	// StepErrors holds per-step failures. Non-empty means results are partial.
	StepErrors []string
}

// Executor runs a planner.Plan against the appropriate cluster agent.
type Executor struct {
	registry *Registry
}

func NewExecutor(r *Registry) *Executor { return &Executor{registry: r} }

// Execute runs all steps in the plan in order and collects the results.
func (e *Executor) Execute(ctx context.Context, plan planner.Plan) (ExecutionResult, error) {
	client, err := e.registry.Client(plan.ClusterName)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("get client for %q: %w", plan.ClusterName, err)
	}

	var result ExecutionResult

	for _, step := range plan.Steps {
		if err := e.runStep(ctx, client, step, &result); err != nil {
			msg := fmt.Sprintf("step %s failed: %s", step.Method, err.Error())
			slog.Warn(msg, "cluster", plan.ClusterName)
			result.StepErrors = append(result.StepErrors, msg)
		}
	}
	return result, nil
}

func (e *Executor) runStep(ctx context.Context, client pb.ClusterAgentServiceClient, step planner.Step, result *ExecutionResult) error {
	switch step.Method {
	case planner.MethodListEvents:
		resp, err := client.ListEvents(ctx, &pb.ListEventsRequest{
			Namespace: step.Namespace,
			Reason:    step.Reason,
			Since:     toTimestampPB(step.Since),
			Limit:     int32(step.Limit),
		})
		if err != nil {
			return err
		}
		result.Events = append(result.Events, fromProtoEvents(resp.Events)...)

	case planner.MethodGetEventsForObject:
		resp, err := client.GetEventsForObject(ctx, &pb.GetEventsForObjectRequest{
			Namespace: step.Namespace,
			Kind:      step.Kind,
			Name:      step.Name,
		})
		if err != nil {
			return err
		}
		result.Events = append(result.Events, fromProtoEvents(resp.Events)...)

	case planner.MethodGetNamespaceSummary:
		resp, err := client.GetNamespaceSummary(ctx, &pb.GetNamespaceSummaryRequest{
			Namespace: step.Namespace,
			Since:     toTimestampPB(step.Since),
		})
		if err != nil {
			return err
		}
		result.NamespaceSummaries = append(result.NamespaceSummaries, resp.Groups...)

	case planner.MethodResolveOwnerChain:
		resp, err := client.ResolveOwnerChain(ctx, &pb.ResolveOwnerChainRequest{
			Namespace: step.Namespace,
			Kind:      step.Kind,
			Name:      step.Name,
		})
		if err != nil {
			return err
		}
		result.Chains = append(result.Chains, resp.Chain)

	case planner.MethodGetObjectSnapshot:
		resp, err := client.GetObjectSnapshot(ctx, &pb.GetObjectSnapshotRequest{
			Namespace: step.Namespace,
			Kind:      step.Kind,
			Name:      step.Name,
		})
		if err != nil {
			return err
		}
		if resp.Snapshot != nil {
			result.Snapshots = append(result.Snapshots, resp.Snapshot)
		}

	default:
		return fmt.Errorf("unknown method: %s", step.Method)
	}
	return nil
}

// ── Proto conversions ──────────────────────────────────────────────────────────

func fromProtoEvents(pbEvs []*pb.NormalizedEvent) []events.NormalizedEvent {
	out := make([]events.NormalizedEvent, 0, len(pbEvs))
	for _, e := range pbEvs {
		out = append(out, fromProtoEvent(e))
	}
	return out
}

func fromProtoEvent(e *pb.NormalizedEvent) events.NormalizedEvent {
	return events.NormalizedEvent{
		ClusterName:        e.ClusterName,
		Namespace:          e.Namespace,
		InvolvedObjectKind: e.InvolvedObjectKind,
		InvolvedObjectName: e.InvolvedObjectName,
		InvolvedObjectUID:  e.InvolvedObjectUid,
		OwnerChain:         fromProtoOwnerChain(e.OwnerChain),
		Reason:             e.Reason,
		Message:            e.Message,
		Count:              e.Count,
		FirstSeen:          timeOrZero(e.FirstSeen),
		LastSeen:           timeOrZero(e.LastSeen),
	}
}

func fromProtoOwnerChain(chain []*pb.OwnerRef) []events.OwnerRef {
	out := make([]events.OwnerRef, len(chain))
	for i, r := range chain {
		out[i] = events.OwnerRef{Kind: r.Kind, Name: r.Name, UID: r.Uid}
	}
	return out
}

func timeOrZero(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

func toTimestampPB(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

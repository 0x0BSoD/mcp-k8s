package coordinator

import (
	"fmt"
	"strings"
	"time"

	"github.com/0x0BSoD/mcp-k8s/pkg/correlation"
	pb "github.com/0x0BSoD/mcp-k8s/proto/gen/clusteragentpb"
)

type ResponseMode string

const (
	ResponseModeTechnical       ResponseMode = "technical"
	ResponseModeConcise         ResponseMode = "concise"
	ResponseModeIncidentSummary ResponseMode = "incident-summary"
)

// NamespaceSummaryLine is a single reason-group entry from GetNamespaceSummary.
type NamespaceSummaryLine struct {
	Reason     string    `json:"reason"`
	TotalCount int32     `json:"total_count"`
	Objects    []string  `json:"objects"`
	LastSeen   time.Time `json:"last_seen"`
}

// ContainerStatusLine is the per-container state included in snapshot output.
type ContainerStatusLine struct {
	Name                    string `json:"name"`
	Ready                   bool   `json:"ready"`
	RestartCount            int32  `json:"restart_count"`
	State                   string `json:"state"`
	Reason                  string `json:"reason,omitempty"`
	LastTerminationExitCode int32  `json:"last_termination_exit_code,omitempty"`
	LastTerminationReason   string `json:"last_termination_reason,omitempty"`
	LastTerminationMessage  string `json:"last_termination_message,omitempty"`
}

// WorkloadConditionLine is a single condition entry from a workload snapshot.
type WorkloadConditionLine struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// SnapshotLine summarises one object snapshot for the response.
type SnapshotLine struct {
	Kind               string                  `json:"kind"`
	Name               string                  `json:"name"`
	Namespace          string                  `json:"namespace"`
	Phase              string                  `json:"phase,omitempty"`
	ContainerStatuses  []ContainerStatusLine   `json:"container_statuses,omitempty"`
	WorkloadConditions []WorkloadConditionLine `json:"workload_conditions,omitempty"`
}

// ContainerLogLine pairs identity with log text.
type ContainerLogLine struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Logs      string `json:"logs"`
}

// ContainerMetricsLine is the per-container CPU/memory usage.
type ContainerMetricsLine struct {
	Name          string `json:"name"`
	CPUMillicores int64  `json:"cpu_millicores"`
	MemoryBytes   int64  `json:"memory_bytes"`
}

// PodMetricsLine is the resource usage snapshot for a single pod.
type PodMetricsLine struct {
	Namespace  string                 `json:"namespace"`
	Pod        string                 `json:"pod"`
	Containers []ContainerMetricsLine `json:"containers"`
	Timestamp  time.Time              `json:"timestamp"`
}

// EventLine is a single row in the rendered timeline.
type EventLine struct {
	Time        time.Time `json:"time"`
	Namespace   string    `json:"namespace"`
	Object      string    `json:"object"`
	Reason      string    `json:"reason"`
	Message     string    `json:"message"`
	Count       int32     `json:"count"`
	IsRootCause bool      `json:"is_root_cause,omitempty"`
	CausedBy    string    `json:"caused_by,omitempty"`
	GroupID     string    `json:"group_id,omitempty"`
}

// Response is the final coordinator output, serialised to the caller.
type Response struct {
	ClusterName      string                 `json:"cluster"`
	Mode             ResponseMode           `json:"mode"`
	NamespaceSummary []NamespaceSummaryLine `json:"namespace_summary,omitempty"`
	Timeline         []EventLine            `json:"timeline"`
	RootCauses       []EventLine            `json:"root_causes"`
	AffectedObjects  []string               `json:"affected_objects,omitempty"`
	Snapshots        []SnapshotLine         `json:"snapshots,omitempty"`
	ContainerLogs    []ContainerLogLine     `json:"container_logs,omitempty"`
	PodMetrics       []PodMetricsLine       `json:"pod_metrics,omitempty"`
	Summary          string                 `json:"summary,omitempty"`
	// Warnings lists any step-level failures; non-empty means results are partial.
	Warnings []string `json:"warnings,omitempty"`
}

// ResponseBuilder formats a correlation.Timeline into a Response.
type ResponseBuilder struct{}

func NewResponseBuilder() *ResponseBuilder { return &ResponseBuilder{} }

// Build renders the timeline according to the requested mode.
func (rb *ResponseBuilder) Build(clusterName string, mode ResponseMode, tl correlation.Timeline, result ExecutionResult) Response {
	resp := Response{
		ClusterName:      clusterName,
		Mode:             mode,
		Warnings:         result.StepErrors,
		NamespaceSummary: toNamespaceSummaryLines(result.NamespaceSummaries),
		Snapshots:        toSnapshotLines(result.Snapshots),
		ContainerLogs:    toContainerLogLines(result.ContainerLogs),
		PodMetrics:       toPodMetricsLines(result.PodMetrics),
	}

	switch mode {
	case ResponseModeConcise:
		resp.RootCauses = toEventLines(tl.RootCauses)
		resp.Summary = rb.summarise(tl)

	case ResponseModeIncidentSummary:
		resp.Timeline = toEventLines(tl.Events)
		resp.RootCauses = toEventLines(tl.RootCauses)
		resp.AffectedObjects = affectedObjects(tl)
		resp.Summary = rb.summarise(tl)

	default: // ResponseModeTechnical
		resp.Timeline = toEventLines(tl.Events)
		resp.RootCauses = toEventLines(tl.RootCauses)
		resp.AffectedObjects = affectedObjects(tl)
	}

	return resp
}

func toEventLines(ces []correlation.CorrelatedEvent) []EventLine {
	out := make([]EventLine, len(ces))
	for i, ce := range ces {
		out[i] = EventLine{
			Time:        ce.LastSeen,
			Namespace:   ce.Namespace,
			Object:      ce.InvolvedObjectKind + "/" + ce.InvolvedObjectName,
			Reason:      ce.Reason,
			Message:     ce.Message,
			Count:       ce.Count,
			IsRootCause: ce.IsRootCause,
			CausedBy:    ce.CausedBy,
			GroupID:     string(ce.GroupID),
		}
	}
	return out
}

func affectedObjects(tl correlation.Timeline) []string {
	seen := make(map[string]struct{})
	for _, ce := range tl.Events {
		key := ce.InvolvedObjectKind + "/" + ce.InvolvedObjectName
		seen[key] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func toSnapshotLines(snaps []*pb.ObjectSnapshot) []SnapshotLine {
	if len(snaps) == 0 {
		return nil
	}
	out := make([]SnapshotLine, len(snaps))
	for i, s := range snaps {
		line := SnapshotLine{
			Kind:      s.Kind,
			Name:      s.Name,
			Namespace: s.Namespace,
			Phase:     s.Phase,
		}
		for _, cs := range s.ContainerStatuses {
			line.ContainerStatuses = append(line.ContainerStatuses, ContainerStatusLine{
				Name:                    cs.Name,
				Ready:                   cs.Ready,
				RestartCount:            cs.RestartCount,
				State:                   cs.State,
				Reason:                  cs.Reason,
				LastTerminationExitCode: cs.LastTerminationExitCode,
				LastTerminationReason:   cs.LastTerminationReason,
				LastTerminationMessage:  cs.LastTerminationMessage,
			})
		}
		for _, wc := range s.WorkloadConditions {
			line.WorkloadConditions = append(line.WorkloadConditions, WorkloadConditionLine{
				Type:    wc.Type,
				Status:  wc.Status,
				Reason:  wc.Reason,
				Message: wc.Message,
			})
		}
		out[i] = line
	}
	return out
}

func toContainerLogLines(logs []ContainerLogEntry) []ContainerLogLine {
	if len(logs) == 0 {
		return nil
	}
	out := make([]ContainerLogLine, len(logs))
	for i, l := range logs {
		out[i] = ContainerLogLine{Pod: l.Pod, Container: l.Container, Logs: l.Logs}
	}
	return out
}

func toNamespaceSummaryLines(groups []*pb.NamespaceEventGroup) []NamespaceSummaryLine {
	if len(groups) == 0 {
		return nil
	}
	out := make([]NamespaceSummaryLine, len(groups))
	for i, g := range groups {
		var lastSeen time.Time
		if g.LastSeen != nil {
			lastSeen = g.LastSeen.AsTime()
		}
		out[i] = NamespaceSummaryLine{
			Reason:     g.Reason,
			TotalCount: g.TotalCount,
			Objects:    g.Objects,
			LastSeen:   lastSeen,
		}
	}
	return out
}

func toPodMetricsLines(pods []*pb.PodMetrics) []PodMetricsLine {
	if len(pods) == 0 {
		return nil
	}
	out := make([]PodMetricsLine, len(pods))
	for i, p := range pods {
		line := PodMetricsLine{
			Namespace: p.Namespace,
			Pod:       p.Pod,
		}
		if p.Timestamp != nil {
			line.Timestamp = p.Timestamp.AsTime()
		}
		for _, c := range p.Containers {
			line.Containers = append(line.Containers, ContainerMetricsLine{
				Name:          c.Name,
				CPUMillicores: c.CpuMillicores,
				MemoryBytes:   c.MemoryBytes,
			})
		}
		out[i] = line
	}
	return out
}

// summarise produces a one-paragraph human-readable explanation.
func (rb *ResponseBuilder) summarise(tl correlation.Timeline) string {
	if len(tl.Events) == 0 {
		return "No events found for the given query."
	}
	if len(tl.RootCauses) == 0 {
		return fmt.Sprintf("Found %d events; no clear root cause identified.", len(tl.Events))
	}

	rcReasons := make([]string, 0, len(tl.RootCauses))
	seen := make(map[string]struct{})
	for _, rc := range tl.RootCauses {
		if _, ok := seen[rc.Reason]; !ok {
			rcReasons = append(rcReasons, rc.Reason)
			seen[rc.Reason] = struct{}{}
		}
	}

	objects := affectedObjects(tl)

	return fmt.Sprintf(
		"Detected %d events across %d object(s). Probable root cause(s): %s. "+
			"Downstream effects: %d additional event(s) observed. Affected: %s.",
		len(tl.Events),
		len(objects),
		strings.Join(rcReasons, ", "),
		len(tl.Events)-len(tl.RootCauses),
		strings.Join(objects, ", "),
	)
}

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

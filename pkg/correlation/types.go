package correlation

import "github.com/0x0BSoD/mcp-k8s/pkg/events"

// GroupID identifies a workload group — derived from the root of the owner chain.
type GroupID string

// CorrelatedEvent is a NormalizedEvent annotated with correlation metadata.
type CorrelatedEvent struct {
	events.NormalizedEvent

	// GroupID is the workload this event belongs to (root of owner chain).
	GroupID GroupID

	// IsRootCause is true when no known causal predecessor exists earlier
	// in the same group's timeline.
	IsRootCause bool

	// CausedBy is the Reason of the event that triggered this one,
	// or empty if this is a root cause or isolated event.
	CausedBy string
}

// Timeline is the output of Correlate: events in chronological order,
// annotated with causal group membership and root cause flags.
type Timeline struct {
	// Events ordered by LastSeen ascending.
	Events []CorrelatedEvent

	// RootCauses is the subset of Events where IsRootCause == true,
	// in the same chronological order.
	RootCauses []CorrelatedEvent
}

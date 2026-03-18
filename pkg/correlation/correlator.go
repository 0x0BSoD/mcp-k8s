package correlation

import (
	"sort"

	"github.com/0x0BSoD/mcp-k8s/pkg/events"
)

// Correlator groups events by workload, identifies causal chains within each
// group, and annotates root causes.
type Correlator struct{}

func New() *Correlator { return &Correlator{} }

// Correlate processes a flat slice of NormalizedEvents and returns a Timeline.
// Events may come from multiple namespaces or objects; the correlator groups
// them by the root of their owner chain before analysing causal order.
func (c *Correlator) Correlate(evs []events.NormalizedEvent) Timeline {
	if len(evs) == 0 {
		return Timeline{}
	}

	// Sort globally by LastSeen ascending so that causal ordering is correct
	// both within groups and in the final output.
	sorted := make([]events.NormalizedEvent, len(evs))
	copy(sorted, evs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].LastSeen.Before(sorted[j].LastSeen)
	})

	// Group by workload (root of owner chain or the involved object itself).
	groups := groupByWorkload(sorted)

	// Annotate each group.
	annotated := make(map[GroupID][]CorrelatedEvent, len(groups))
	for gid, gevs := range groups {
		annotated[gid] = annotateGroup(gid, gevs)
	}

	// Flatten back into chronological order.
	var all []CorrelatedEvent
	for _, ces := range annotated {
		all = append(all, ces...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].LastSeen.Before(all[j].LastSeen)
	})

	var rootCauses []CorrelatedEvent
	for _, ce := range all {
		if ce.IsRootCause {
			rootCauses = append(rootCauses, ce)
		}
	}

	return Timeline{Events: all, RootCauses: rootCauses}
}

// groupByWorkload assigns each event to a group identified by the root of its
// owner chain. When no owner chain is present, the event's own UID is used.
func groupByWorkload(evs []events.NormalizedEvent) map[GroupID][]events.NormalizedEvent {
	groups := make(map[GroupID][]events.NormalizedEvent)
	for _, e := range evs {
		gid := groupIDFor(e)
		groups[gid] = append(groups[gid], e)
	}
	return groups
}

// groupIDFor returns the GroupID for an event: the UID of the topmost owner,
// falling back to the involved object's UID, then to Kind/Name.
func groupIDFor(e events.NormalizedEvent) GroupID {
	if len(e.OwnerChain) > 0 {
		root := e.OwnerChain[len(e.OwnerChain)-1]
		if root.UID != "" {
			return GroupID(root.UID)
		}
		return GroupID(root.Kind + "/" + root.Name)
	}
	if e.InvolvedObjectUID != "" {
		return GroupID(e.InvolvedObjectUID)
	}
	return GroupID(e.InvolvedObjectKind + "/" + e.InvolvedObjectName)
}

// annotateGroup processes events that belong to the same workload group.
// It walks the timeline in order, tracking which reasons have been seen,
// and marks each event as either a root cause or downstream of a prior event.
func annotateGroup(gid GroupID, evs []events.NormalizedEvent) []CorrelatedEvent {
	seenReasons := make(map[string]struct{})
	out := make([]CorrelatedEvent, 0, len(evs))

	for _, e := range evs {
		cause := knownCause(e.Reason, seenReasons)
		ce := CorrelatedEvent{
			NormalizedEvent: e,
			GroupID:         gid,
			CausedBy:        cause,
			IsRootCause:     cause == "",
		}
		seenReasons[e.Reason] = struct{}{}
		out = append(out, ce)
	}
	return out
}

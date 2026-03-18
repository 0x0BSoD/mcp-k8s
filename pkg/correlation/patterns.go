package correlation

// causalEdges maps a reason to the set of reasons it commonly causes.
// Edges represent "A typically precedes and causes B" in a Kubernetes failure sequence.
var causalEdges = map[string][]string{
	// Scheduling failures
	"FailedScheduling": {"Pending"},

	// Image issues
	"ErrImagePull":     {"ImagePullBackOff", "BackOff"},
	"ImagePullBackOff": {"BackOff"},

	// Volume / mount failures
	"FailedMount":        {"BackOff", "Unhealthy"},
	"FailedAttachVolume": {"FailedMount"},
	"VolumeResizeFailed": {"FailedMount"},

	// Probe failures and container lifecycle
	"Unhealthy": {"Killing", "BackOff"},
	"Killing":   {"BackOff"},
	"Failed":    {"BackOff"},

	// OOM
	"OOMKilling": {"BackOff"},

	// Back-off chain
	"BackOff":          {"CrashLoopBackOff"},
	"CrashLoopBackOff": {"FailedCreate"},

	// ReplicaSet / Deployment degradation
	"FailedCreate":      {"ScalingReplicaSet"},
	"ScalingReplicaSet": {},

	// Node-level
	"NetworkNotReady": {"FailedScheduling"},
	"NodeNotReady":    {"FailedScheduling"},
}

// causedByIndex is the inverse of causalEdges:
// maps a reason B → set of reasons A that are known to cause B.
// Built once at package init.
var causedByIndex map[string][]string

func init() {
	causedByIndex = make(map[string][]string)
	for cause, effects := range causalEdges {
		for _, effect := range effects {
			causedByIndex[effect] = append(causedByIndex[effect], cause)
		}
	}
}

// knownCause returns the first predecessor reason found in seenReasons,
// or "" if this event has no known cause in the current group timeline.
func knownCause(reason string, seenReasons map[string]struct{}) string {
	for _, pred := range causedByIndex[reason] {
		if _, ok := seenReasons[pred]; ok {
			return pred
		}
	}
	return ""
}

package planner

import "time"

// Intent describes what the user is trying to find out.
type Intent int

const (
	// IntentUnknown: planner falls back to heuristics based on available params.
	IntentUnknown Intent = iota
	// IntentNamespaceOverview: high-level view of what is happening in a namespace.
	IntentNamespaceOverview
	// IntentExplainObjectIssue: why is a specific object unhealthy?
	// Requires ObjectKind + ObjectName.
	IntentExplainObjectIssue
	// IntentExplainRestarts: why are pods restarting in a namespace?
	IntentExplainRestarts
	// IntentBuildTimeline: all events for a namespace over a time range, ordered.
	IntentBuildTimeline
)

// Query is the structured input to the planner.
// It is produced by the coordinator's gateway after parsing the user request.
type Query struct {
	ClusterName string
	Namespace   string
	ObjectKind  string    // optional: constrains object-level steps
	ObjectName  string    // optional: constrains object-level steps
	Reason      string    // optional: pre-filter by event reason
	Since       time.Time // zero = no lower bound
	Limit       int       // 0 = no limit on individual steps
	Intent      Intent
}

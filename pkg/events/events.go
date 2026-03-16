package events

import "time"

// OwnerRef is a single link in the owner chain.
type OwnerRef struct {
	Kind string
	Name string
	UID  string
}

// NormalizedEvent is an enriched, deduplicated Kubernetes event.
type NormalizedEvent struct {
	ClusterName        string
	Namespace          string
	InvolvedObjectKind string
	InvolvedObjectName string
	InvolvedObjectUID  string
	OwnerChain         []OwnerRef
	Reason             string
	Message            string // normalized/trimmed
	Count              int32
	FirstSeen          time.Time
	LastSeen           time.Time
}

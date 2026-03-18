package events

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// Normalize converts a raw Kubernetes Event into a NormalizedEvent.
// The clusterName is injected by the caller (not available inside the event itself).
func Normalize(clusterName string, e *corev1.Event) NormalizedEvent {
	firstSeen := e.FirstTimestamp.Time
	lastSeen := e.LastTimestamp.Time

	// EventTime (newer field) takes precedence when set.
	if !e.EventTime.IsZero() {
		if firstSeen.IsZero() {
			firstSeen = e.EventTime.Time
		}
		lastSeen = e.EventTime.Time
	}

	count := e.Count
	if count == 0 {
		count = 1
	}

	return NormalizedEvent{
		ClusterName:        clusterName,
		Namespace:          e.Namespace,
		InvolvedObjectKind: e.InvolvedObject.Kind,
		InvolvedObjectName: e.InvolvedObject.Name,
		InvolvedObjectUID:  string(e.InvolvedObject.UID),
		Reason:             e.Reason,
		Message:            normalizeMessage(e.Message),
		Count:              count,
		FirstSeen:          firstSeen,
		LastSeen:           lastSeen,
		// OwnerChain is populated by the enricher after normalization.
	}
}

// normalizeMessage trims whitespace and collapses repeated spaces.
func normalizeMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	parts := strings.Fields(msg)
	return strings.Join(parts, " ")
}

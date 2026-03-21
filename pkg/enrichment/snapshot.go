package enrichment

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0x0BSoD/mcp-k8s/pkg/events"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const maxTerminationMessageLen = 256

// ObjectSnapshot is the current state of a Kubernetes object, suitable for
// returning to the coordinator without exposing raw API types.
type ObjectSnapshot struct {
	Namespace          string
	Kind               string
	Name               string
	UID                string
	Phase              string
	Labels             map[string]string
	Annotations        map[string]string
	OwnerChain         []events.OwnerRef
	RawStatusJSON      string
	ContainerStatuses  []ContainerStatus
	WorkloadConditions []WorkloadCondition
}

// ContainerStatus holds structured state for one container.
type ContainerStatus struct {
	Name                    string
	Ready                   bool
	RestartCount            int32
	State                   string // running | waiting | terminated
	Reason                  string // e.g. CrashLoopBackOff, OOMKilled
	LastTerminationExitCode int32
	LastTerminationReason   string
	LastTerminationMessage  string
}

// WorkloadCondition mirrors a Kubernetes condition entry.
type WorkloadCondition struct {
	Type    string // e.g. Available, Progressing, ReplicaFailure
	Status  string // True | False | Unknown
	Reason  string
	Message string
}

// GetObjectSnapshot fetches the current state of an object and returns a snapshot.
func (e *Enricher) GetObjectSnapshot(ctx context.Context, namespace, kind, name string) (*ObjectSnapshot, error) {
	snap, err := e.fetchSnapshot(ctx, namespace, kind, name)
	if err != nil {
		return nil, err
	}

	chain, err := e.ResolveOwnerChain(ctx, namespace, kind, name)
	if err != nil {
		snap.OwnerChain = nil
	} else {
		snap.OwnerChain = chain
	}

	return snap, nil
}

func (e *Enricher) fetchSnapshot(ctx context.Context, namespace, kind, name string) (*ObjectSnapshot, error) {
	c := e.client

	switch kind {
	case "Pod":
		obj, err := c.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get pod: %w", err)
		}
		statusJSON, _ := json.Marshal(obj.Status)
		return &ObjectSnapshot{
			Namespace:         namespace,
			Kind:              kind,
			Name:              name,
			UID:               string(obj.UID),
			Phase:             string(obj.Status.Phase),
			Labels:            obj.Labels,
			Annotations:       obj.Annotations,
			RawStatusJSON:     string(statusJSON),
			ContainerStatuses: extractContainerStatuses(obj.Status.ContainerStatuses),
		}, nil

	case "Deployment":
		obj, err := c.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get deployment: %w", err)
		}
		statusJSON, _ := json.Marshal(obj.Status)
		phase := "Available"
		var conditions []WorkloadCondition
		for _, cond := range obj.Status.Conditions {
			if cond.Type == "Available" && cond.Status != corev1.ConditionTrue {
				phase = "Degraded"
			}
			conditions = append(conditions, WorkloadCondition{
				Type:    string(cond.Type),
				Status:  string(cond.Status),
				Reason:  cond.Reason,
				Message: cond.Message,
			})
		}
		return &ObjectSnapshot{
			Namespace:          namespace,
			Kind:               kind,
			Name:               name,
			UID:                string(obj.UID),
			Phase:              phase,
			Labels:             obj.Labels,
			Annotations:        obj.Annotations,
			RawStatusJSON:      string(statusJSON),
			WorkloadConditions: conditions,
		}, nil

	case "StatefulSet":
		obj, err := c.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get statefulset: %w", err)
		}
		statusJSON, _ := json.Marshal(obj.Status)
		var conditions []WorkloadCondition
		for _, cond := range obj.Status.Conditions {
			conditions = append(conditions, WorkloadCondition{
				Type:    string(cond.Type),
				Status:  string(cond.Status),
				Reason:  cond.Reason,
				Message: cond.Message,
			})
		}
		return &ObjectSnapshot{
			Namespace:          namespace,
			Kind:               kind,
			Name:               name,
			UID:                string(obj.UID),
			Labels:             obj.Labels,
			Annotations:        obj.Annotations,
			RawStatusJSON:      string(statusJSON),
			WorkloadConditions: conditions,
		}, nil

	case "DaemonSet":
		obj, err := c.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get daemonset: %w", err)
		}
		statusJSON, _ := json.Marshal(obj.Status)
		var conditions []WorkloadCondition
		for _, cond := range obj.Status.Conditions {
			conditions = append(conditions, WorkloadCondition{
				Type:    string(cond.Type),
				Status:  string(cond.Status),
				Reason:  cond.Reason,
				Message: cond.Message,
			})
		}
		return &ObjectSnapshot{
			Namespace:          namespace,
			Kind:               kind,
			Name:               name,
			UID:                string(obj.UID),
			Labels:             obj.Labels,
			Annotations:        obj.Annotations,
			RawStatusJSON:      string(statusJSON),
			WorkloadConditions: conditions,
		}, nil

	case "ReplicaSet":
		obj, err := c.AppsV1().ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get replicaset: %w", err)
		}
		statusJSON, _ := json.Marshal(obj.Status)
		return &ObjectSnapshot{
			Namespace:     namespace,
			Kind:          kind,
			Name:          name,
			UID:           string(obj.UID),
			Labels:        obj.Labels,
			Annotations:   obj.Annotations,
			RawStatusJSON: string(statusJSON),
		}, nil

	case "Job":
		obj, err := c.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get job: %w", err)
		}
		statusJSON, _ := json.Marshal(obj.Status)
		var conditions []WorkloadCondition
		for _, cond := range obj.Status.Conditions {
			conditions = append(conditions, WorkloadCondition{
				Type:    string(cond.Type),
				Status:  string(cond.Status),
				Reason:  cond.Reason,
				Message: cond.Message,
			})
		}
		return &ObjectSnapshot{
			Namespace:          namespace,
			Kind:               kind,
			Name:               name,
			UID:                string(obj.UID),
			Labels:             obj.Labels,
			Annotations:        obj.Annotations,
			RawStatusJSON:      string(statusJSON),
			WorkloadConditions: conditions,
		}, nil

	case "CronJob":
		obj, err := c.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get cronjob: %w", err)
		}
		statusJSON, _ := json.Marshal(obj.Status)
		return &ObjectSnapshot{
			Namespace:     namespace,
			Kind:          kind,
			Name:          name,
			UID:           string(obj.UID),
			Labels:        obj.Labels,
			Annotations:   obj.Annotations,
			RawStatusJSON: string(statusJSON),
		}, nil

	case "PersistentVolumeClaim":
		obj, err := c.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get pvc: %w", err)
		}
		statusJSON, _ := json.Marshal(obj.Status)
		return &ObjectSnapshot{
			Namespace:     namespace,
			Kind:          kind,
			Name:          name,
			UID:           string(obj.UID),
			Phase:         string(obj.Status.Phase),
			Labels:        obj.Labels,
			Annotations:   obj.Annotations,
			RawStatusJSON: string(statusJSON),
		}, nil

	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

func extractContainerStatuses(statuses []corev1.ContainerStatus) []ContainerStatus {
	out := make([]ContainerStatus, 0, len(statuses))
	for _, cs := range statuses {
		s := ContainerStatus{
			Name:         cs.Name,
			Ready:        cs.Ready,
			RestartCount: cs.RestartCount,
		}

		switch {
		case cs.State.Running != nil:
			s.State = "running"
		case cs.State.Waiting != nil:
			s.State = "waiting"
			s.Reason = cs.State.Waiting.Reason
		case cs.State.Terminated != nil:
			s.State = "terminated"
			s.Reason = cs.State.Terminated.Reason
		}

		if t := cs.LastTerminationState.Terminated; t != nil {
			s.LastTerminationExitCode = t.ExitCode
			s.LastTerminationReason = t.Reason
			msg := t.Message
			if len(msg) > maxTerminationMessageLen {
				msg = msg[:maxTerminationMessageLen]
			}
			s.LastTerminationMessage = msg
		}

		out = append(out, s)
	}
	return out
}

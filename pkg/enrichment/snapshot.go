package enrichment

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0x0BSoD/mcp-k8s/pkg/events"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ObjectSnapshot is the current state of a Kubernetes object, suitable for
// returning to the coordinator without exposing raw API types.
type ObjectSnapshot struct {
	Namespace     string
	Kind          string
	Name          string
	UID           string
	Phase         string
	Labels        map[string]string
	Annotations   map[string]string
	OwnerChain    []events.OwnerRef
	RawStatusJSON string
}

// GetObjectSnapshot fetches the current state of an object and returns a snapshot.
func (e *Enricher) GetObjectSnapshot(ctx context.Context, namespace, kind, name string) (*ObjectSnapshot, error) {
	snap, err := e.fetchSnapshot(ctx, namespace, kind, name)
	if err != nil {
		return nil, err
	}

	chain, err := e.ResolveOwnerChain(ctx, namespace, kind, name)
	if err != nil {
		// Non-fatal: return snapshot without chain rather than failing.
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
			Namespace:     namespace,
			Kind:          kind,
			Name:          name,
			UID:           string(obj.UID),
			Phase:         string(obj.Status.Phase),
			Labels:        obj.Labels,
			Annotations:   obj.Annotations,
			RawStatusJSON: string(statusJSON),
		}, nil

	case "Deployment":
		obj, err := c.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get deployment: %w", err)
		}
		statusJSON, _ := json.Marshal(obj.Status)
		phase := "Available"
		for _, cond := range obj.Status.Conditions {
			if cond.Type == "Available" && cond.Status != corev1.ConditionTrue {
				phase = "Degraded"
				break
			}
		}
		return &ObjectSnapshot{
			Namespace:     namespace,
			Kind:          kind,
			Name:          name,
			UID:           string(obj.UID),
			Phase:         phase,
			Labels:        obj.Labels,
			Annotations:   obj.Annotations,
			RawStatusJSON: string(statusJSON),
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
			Phase:         "",
			Labels:        obj.Labels,
			Annotations:   obj.Annotations,
			RawStatusJSON: string(statusJSON),
		}, nil

	case "StatefulSet":
		obj, err := c.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get statefulset: %w", err)
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

	case "DaemonSet":
		obj, err := c.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get daemonset: %w", err)
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
		return &ObjectSnapshot{
			Namespace:     namespace,
			Kind:          kind,
			Name:          name,
			UID:           string(obj.UID),
			Labels:        obj.Labels,
			Annotations:   obj.Annotations,
			RawStatusJSON: string(statusJSON),
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

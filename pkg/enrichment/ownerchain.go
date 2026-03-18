package enrichment

import (
	"context"
	"fmt"

	"github.com/0x0BSoD/mcp-k8s/pkg/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const maxChainDepth = 10

// ResolveOwnerChain walks ownerReferences upward from the given object
// and returns the chain ordered leaf → root.
// e.g. Pod → ReplicaSet → Deployment
func (e *Enricher) ResolveOwnerChain(ctx context.Context, namespace, kind, name string) ([]events.OwnerRef, error) {
	var chain []events.OwnerRef
	currentKind := kind
	currentName := name

	for i := 0; i < maxChainDepth; i++ {
		owners, uid, err := e.getOwnerRefs(ctx, namespace, currentKind, currentName)
		if err != nil {
			return chain, fmt.Errorf("fetch %s/%s: %w", currentKind, currentName, err)
		}

		chain = append(chain, events.OwnerRef{
			Kind: currentKind,
			Name: currentName,
			UID:  uid,
		})

		if len(owners) == 0 {
			break
		}

		// Follow the first controlling owner (controller=true preferred).
		next := owners[0]
		for _, o := range owners {
			if o.Controller != nil && *o.Controller {
				next = o
				break
			}
		}

		currentKind = next.Kind
		currentName = next.Name
	}

	return chain, nil
}

// getOwnerRefs fetches an object by kind+name and returns its ownerReferences + its own UID.
func (e *Enricher) getOwnerRefs(ctx context.Context, namespace, kind, name string) ([]metav1.OwnerReference, string, error) {
	c := e.client

	switch kind {
	case "Pod":
		obj, err := c.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		return obj.OwnerReferences, string(obj.UID), nil

	case "ReplicaSet":
		obj, err := c.AppsV1().ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		return obj.OwnerReferences, string(obj.UID), nil

	case "Deployment":
		obj, err := c.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		return obj.OwnerReferences, string(obj.UID), nil

	case "StatefulSet":
		obj, err := c.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		return obj.OwnerReferences, string(obj.UID), nil

	case "DaemonSet":
		obj, err := c.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		return obj.OwnerReferences, string(obj.UID), nil

	case "Job":
		obj, err := c.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		return obj.OwnerReferences, string(obj.UID), nil

	case "CronJob":
		obj, err := c.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		return obj.OwnerReferences, string(obj.UID), nil

	case "PersistentVolumeClaim":
		obj, err := c.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		// PVCs don't use ownerReferences to reach PVs; the link is spec.volumeName.
		// Caller can use GetObjectSnapshot to inspect the PVC and find the bound PV.
		return obj.OwnerReferences, string(obj.UID), nil

	default:
		// Unknown kind: return empty chain without error — we just stop here.
		return nil, "", nil
	}
}

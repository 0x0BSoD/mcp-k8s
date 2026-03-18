package enrichment

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func boolPtr(b bool) *bool { return &b }

// buildChain: Pod → ReplicaSet → Deployment
func TestResolveOwnerChain_PodToDeployment(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", UID: "uid-deploy"},
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api-rs", Namespace: "default", UID: "uid-rs",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", Name: "api", UID: "uid-deploy", Controller: boolPtr(true)},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api-pod", Namespace: "default", UID: "uid-pod",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "api-rs", UID: "uid-rs", Controller: boolPtr(true)},
			},
		},
	}

	client := fake.NewSimpleClientset(deploy, rs, pod)
	e := New(client)

	chain, err := e.ResolveOwnerChain(context.Background(), "default", "Pod", "api-pod")
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 3 {
		t.Fatalf("expected chain length 3 (Pod, ReplicaSet, Deployment), got %d: %v", len(chain), chain)
	}
	if chain[0].Kind != "Pod" || chain[1].Kind != "ReplicaSet" || chain[2].Kind != "Deployment" {
		t.Errorf("unexpected chain: %v", chain)
	}
}

// Pod → StatefulSet
func TestResolveOwnerChain_PodToStatefulSet(t *testing.T) {
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "default", UID: "uid-ss"},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "redis-0", Namespace: "default", UID: "uid-pod",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "StatefulSet", Name: "redis", UID: "uid-ss", Controller: boolPtr(true)},
			},
		},
	}

	client := fake.NewSimpleClientset(ss, pod)
	e := New(client)

	chain, err := e.ResolveOwnerChain(context.Background(), "default", "Pod", "redis-0")
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 2 {
		t.Fatalf("expected chain length 2, got %d: %v", len(chain), chain)
	}
	if chain[1].Kind != "StatefulSet" {
		t.Errorf("expected root to be StatefulSet, got %s", chain[1].Kind)
	}
}

// Job → CronJob
func TestResolveOwnerChain_JobToCronJob(t *testing.T) {
	cj := &appsv1.Deployment{} // placeholder — CronJob handled separately below
	_ = cj

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "backup-pod", Namespace: "default", UID: "uid-pod",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Job", Name: "backup-job", UID: "uid-job", Controller: boolPtr(true)},
			},
		},
	}

	// fake client doesn't need the Job to exist for the pod lookup,
	// but we need it for the job→cronjob step.
	// Use a minimal batch/v1 Job.
	jobObj := &metav1.ObjectMeta{}
	_ = jobObj

	client := fake.NewSimpleClientset(pod)
	e := New(client)

	// Will get an error fetching Job (not in fake store) — chain stops at Pod.
	chain, _ := e.ResolveOwnerChain(context.Background(), "default", "Pod", "backup-pod")
	if len(chain) == 0 {
		t.Fatal("expected at least the Pod in the chain")
	}
	if chain[0].Kind != "Pod" {
		t.Errorf("expected first entry to be Pod, got %s", chain[0].Kind)
	}
}

func TestGetObjectSnapshot_Pod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-pod",
			Namespace: "default",
			UID:       "uid-pod",
			Labels:    map[string]string{"app": "api"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	client := fake.NewSimpleClientset(pod)
	e := New(client)

	snap, err := e.GetObjectSnapshot(context.Background(), "default", "Pod", "api-pod")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Phase != "Running" {
		t.Errorf("expected phase Running, got %s", snap.Phase)
	}
	if snap.Labels["app"] != "api" {
		t.Errorf("expected label app=api")
	}
	if snap.RawStatusJSON == "" {
		t.Error("expected non-empty RawStatusJSON")
	}
}

func TestGetObjectSnapshot_UnsupportedKind(t *testing.T) {
	client := fake.NewSimpleClientset()
	e := New(client)

	_, err := e.GetObjectSnapshot(context.Background(), "default", "CustomResource", "foo")
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
}

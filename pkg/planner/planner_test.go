package planner

import (
	"testing"
	"time"
)

var baseQuery = Query{
	ClusterName: "prod-eu",
	Namespace:   "payments",
	Since:       time.Unix(0, 0),
}

func methods(plan Plan) []Method {
	out := make([]Method, len(plan.Steps))
	for i, s := range plan.Steps {
		out[i] = s.Method
	}
	return out
}

func TestPlan_MissingClusterName(t *testing.T) {
	_, err := New().Plan(Query{Namespace: "ns"})
	if err == nil {
		t.Fatal("expected error for missing ClusterName")
	}
}

func TestPlan_NamespaceOverview(t *testing.T) {
	plan, err := New().Plan(Query{
		ClusterName: "prod-eu",
		Namespace:   "payments",
		Intent:      IntentNamespaceOverview,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Method{MethodGetNamespaceSummary, MethodListEvents}
	if got := methods(plan); !equalMethods(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPlan_ExplainObjectIssue(t *testing.T) {
	plan, err := New().Plan(Query{
		ClusterName: "prod-eu",
		Namespace:   "payments",
		ObjectKind:  "Pod",
		ObjectName:  "api-7d9c6",
		Intent:      IntentExplainObjectIssue,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Method{MethodGetEventsForObject, MethodResolveOwnerChain, MethodGetObjectSnapshot}
	if got := methods(plan); !equalMethods(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Steps must carry the object coordinates.
	for _, s := range plan.Steps {
		if s.Kind != "Pod" || s.Name != "api-7d9c6" {
			t.Errorf("step %s missing object coords: kind=%q name=%q", s.Method, s.Kind, s.Name)
		}
	}
}

func TestPlan_ExplainObjectIssue_MissingObject(t *testing.T) {
	_, err := New().Plan(Query{
		ClusterName: "prod-eu",
		Namespace:   "payments",
		Intent:      IntentExplainObjectIssue,
		// ObjectKind + ObjectName intentionally omitted
	})
	if err == nil {
		t.Fatal("expected error when object coords are missing")
	}
}

func TestPlan_ExplainRestarts(t *testing.T) {
	plan, err := New().Plan(Query{
		ClusterName: "prod-eu",
		Namespace:   "payments",
		Intent:      IntentExplainRestarts,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].Method != MethodGetNamespaceSummary {
		t.Errorf("first step should be GetNamespaceSummary, got %s", plan.Steps[0].Method)
	}
	// Should have multiple ListEvents steps with different reasons.
	var listSteps []Step
	for _, s := range plan.Steps {
		if s.Method == MethodListEvents {
			listSteps = append(listSteps, s)
		}
	}
	if len(listSteps) < 2 {
		t.Errorf("expected multiple ListEvents steps for restarts, got %d", len(listSteps))
	}
	// Each ListEvents step must have a non-empty reason.
	for _, s := range listSteps {
		if s.Reason == "" {
			t.Errorf("restart plan ListEvents step has empty reason")
		}
	}
}

func TestPlan_BuildTimeline(t *testing.T) {
	plan, err := New().Plan(Query{
		ClusterName: "prod-eu",
		Namespace:   "payments",
		Since:       time.Now().Add(-time.Hour),
		Limit:       200,
		Intent:      IntentBuildTimeline,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Method != MethodListEvents {
		t.Errorf("expected single ListEvents step, got %v", methods(plan))
	}
	if plan.Steps[0].Limit != 200 {
		t.Errorf("expected limit 200, got %d", plan.Steps[0].Limit)
	}
}

func TestPlan_HeuristicObjectFallback(t *testing.T) {
	plan, err := New().Plan(Query{
		ClusterName: "prod-eu",
		Namespace:   "default",
		ObjectKind:  "Deployment",
		ObjectName:  "redis",
		// Intent deliberately omitted
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Method{MethodGetEventsForObject, MethodResolveOwnerChain, MethodGetObjectSnapshot}
	if got := methods(plan); !equalMethods(got, want) {
		t.Errorf("heuristic: got %v, want %v", got, want)
	}
}

func TestPlan_HeuristicNamespaceFallback(t *testing.T) {
	plan, err := New().Plan(Query{
		ClusterName: "prod-eu",
		Namespace:   "monitoring",
		// Intent + object deliberately omitted
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Method{MethodGetNamespaceSummary, MethodListEvents}
	if got := methods(plan); !equalMethods(got, want) {
		t.Errorf("heuristic: got %v, want %v", got, want)
	}
}

func TestPlan_HeuristicNoParams(t *testing.T) {
	_, err := New().Plan(Query{ClusterName: "prod-eu"})
	if err == nil {
		t.Fatal("expected error when neither namespace nor object is provided")
	}
}

func TestPlan_ClusterNamePropagated(t *testing.T) {
	plan, err := New().Plan(baseQuery)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ClusterName != "prod-eu" {
		t.Errorf("expected ClusterName prod-eu, got %q", plan.ClusterName)
	}
}

func equalMethods(a, b []Method) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

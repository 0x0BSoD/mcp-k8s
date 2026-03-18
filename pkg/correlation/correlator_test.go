package correlation

import (
	"testing"
	"time"

	"github.com/0x0BSoD/mcp-k8s/pkg/events"
)

func makeEvent(uid, reason string, t time.Time, chain ...events.OwnerRef) events.NormalizedEvent {
	return events.NormalizedEvent{
		InvolvedObjectUID:  uid,
		InvolvedObjectKind: "Pod",
		InvolvedObjectName: "pod-" + uid,
		Namespace:          "default",
		Reason:             reason,
		Count:              1,
		FirstSeen:          t,
		LastSeen:           t,
		OwnerChain:         chain,
	}
}

var (
	t0 = time.Unix(1000, 0)
	t1 = t0.Add(time.Minute)
	t2 = t0.Add(2 * time.Minute)
	t3 = t0.Add(3 * time.Minute)
)

// ── Basic causal chain ─────────────────────────────────────────────────────────

func TestCorrelate_Empty(t *testing.T) {
	tl := New().Correlate(nil)
	if len(tl.Events) != 0 {
		t.Fatalf("expected empty timeline, got %d events", len(tl.Events))
	}
}

func TestCorrelate_SingleEvent_IsRootCause(t *testing.T) {
	evs := []events.NormalizedEvent{
		makeEvent("uid-1", "BackOff", t0),
	}
	tl := New().Correlate(evs)
	if len(tl.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(tl.Events))
	}
	if !tl.Events[0].IsRootCause {
		t.Error("isolated event should be marked as root cause")
	}
	if len(tl.RootCauses) != 1 {
		t.Errorf("expected 1 root cause, got %d", len(tl.RootCauses))
	}
}

func TestCorrelate_KnownChain_FailedMount_BackOff(t *testing.T) {
	// FailedMount → BackOff: BackOff should NOT be a root cause.
	deployUID := "deploy-uid"
	chain := []events.OwnerRef{{Kind: "Deployment", Name: "api", UID: deployUID}}

	evs := []events.NormalizedEvent{
		makeEvent("uid-1", "FailedMount", t0, chain...),
		makeEvent("uid-1", "BackOff", t1, chain...),
	}
	tl := New().Correlate(evs)

	if len(tl.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(tl.Events))
	}

	failedMount := tl.Events[0]
	backOff := tl.Events[1]

	if !failedMount.IsRootCause {
		t.Error("FailedMount should be root cause")
	}
	if backOff.IsRootCause {
		t.Error("BackOff should NOT be root cause (caused by FailedMount)")
	}
	if backOff.CausedBy != "FailedMount" {
		t.Errorf("BackOff.CausedBy = %q, want FailedMount", backOff.CausedBy)
	}
}

func TestCorrelate_FullChain_FailedMount_BackOff_CrashLoop(t *testing.T) {
	deployUID := "deploy-uid"
	chain := []events.OwnerRef{{Kind: "Deployment", Name: "api", UID: deployUID}}

	evs := []events.NormalizedEvent{
		makeEvent("uid-1", "FailedMount", t0, chain...),
		makeEvent("uid-1", "BackOff", t1, chain...),
		makeEvent("uid-1", "CrashLoopBackOff", t2, chain...),
	}
	tl := New().Correlate(evs)

	roots := tl.RootCauses
	if len(roots) != 1 || roots[0].Reason != "FailedMount" {
		t.Errorf("expected 1 root cause (FailedMount), got %v", reasons(roots))
	}

	for _, ce := range tl.Events {
		if ce.Reason == "BackOff" && ce.CausedBy != "FailedMount" {
			t.Errorf("BackOff.CausedBy = %q, want FailedMount", ce.CausedBy)
		}
		if ce.Reason == "CrashLoopBackOff" && ce.CausedBy != "BackOff" {
			t.Errorf("CrashLoopBackOff.CausedBy = %q, want BackOff", ce.CausedBy)
		}
	}
}

// ── Grouping ───────────────────────────────────────────────────────────────────

func TestCorrelate_TwoWorkloads_IndependentRootCauses(t *testing.T) {
	// Two separate deployments; each has its own FailedMount.
	// Both FailedMounts should be root causes; BackOffs should not.
	chainA := []events.OwnerRef{{Kind: "Deployment", Name: "svc-a", UID: "deploy-a"}}
	chainB := []events.OwnerRef{{Kind: "Deployment", Name: "svc-b", UID: "deploy-b"}}

	evs := []events.NormalizedEvent{
		makeEvent("uid-a1", "FailedMount", t0, chainA...),
		makeEvent("uid-b1", "FailedMount", t0, chainB...),
		makeEvent("uid-a1", "BackOff", t1, chainA...),
		makeEvent("uid-b1", "BackOff", t1, chainB...),
	}
	tl := New().Correlate(evs)

	if len(tl.RootCauses) != 2 {
		t.Errorf("expected 2 root causes (one per workload), got %d: %v",
			len(tl.RootCauses), reasons(tl.RootCauses))
	}
	for _, rc := range tl.RootCauses {
		if rc.Reason != "FailedMount" {
			t.Errorf("expected root cause FailedMount, got %s", rc.Reason)
		}
	}
}

func TestCorrelate_NoOwnerChain_GroupsByUID(t *testing.T) {
	// Events without owner chain: each unique UID is its own group.
	evs := []events.NormalizedEvent{
		makeEvent("uid-1", "FailedMount", t0),
		makeEvent("uid-1", "BackOff", t1),
		makeEvent("uid-2", "BackOff", t2), // different UID → different group → root cause
	}
	tl := New().Correlate(evs)

	// uid-2's BackOff has no FailedMount in its group → root cause.
	var uid2BackOff *CorrelatedEvent
	for i := range tl.Events {
		if tl.Events[i].InvolvedObjectUID == "uid-2" {
			uid2BackOff = &tl.Events[i]
		}
	}
	if uid2BackOff == nil {
		t.Fatal("uid-2 BackOff not found in timeline")
	}
	if !uid2BackOff.IsRootCause {
		t.Error("uid-2 BackOff should be root cause (isolated group)")
	}
}

// ── Chronological ordering ─────────────────────────────────────────────────────

func TestCorrelate_OutputIsChronological(t *testing.T) {
	evs := []events.NormalizedEvent{
		makeEvent("uid-1", "CrashLoopBackOff", t3),
		makeEvent("uid-1", "FailedMount", t0),
		makeEvent("uid-1", "BackOff", t2),
		makeEvent("uid-1", "Unhealthy", t1),
	}
	tl := New().Correlate(evs)

	for i := 1; i < len(tl.Events); i++ {
		if tl.Events[i].LastSeen.Before(tl.Events[i-1].LastSeen) {
			t.Errorf("timeline not sorted: event[%d] (%s @ %v) before event[%d] (%s @ %v)",
				i, tl.Events[i].Reason, tl.Events[i].LastSeen,
				i-1, tl.Events[i-1].Reason, tl.Events[i-1].LastSeen)
		}
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func reasons(ces []CorrelatedEvent) []string {
	out := make([]string, len(ces))
	for i, ce := range ces {
		out[i] = ce.Reason
	}
	return out
}

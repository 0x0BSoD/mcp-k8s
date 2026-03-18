package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/0x0BSoD/mcp-k8s/pkg/events"
)

func makeEvent(ns, uid, reason string, lastSeen time.Time, count int32) events.NormalizedEvent {
	return events.NormalizedEvent{
		Namespace:          ns,
		InvolvedObjectUID:  uid,
		InvolvedObjectName: fmt.Sprintf("obj-%s", uid),
		Reason:             reason,
		Count:              count,
		FirstSeen:          lastSeen.Add(-time.Minute),
		LastSeen:           lastSeen,
	}
}

func TestStore_AddAndLen(t *testing.T) {
	s := New(100)
	s.Add(makeEvent("default", "uid-1", "BackOff", time.Now(), 3))
	s.Add(makeEvent("default", "uid-2", "OOMKilled", time.Now(), 1))
	if s.Len() != 2 {
		t.Fatalf("expected 2, got %d", s.Len())
	}
}

func TestStore_RingEvictsOldest(t *testing.T) {
	s := New(3)
	t0 := time.Unix(1000, 0)
	s.Add(makeEvent("ns", "uid-1", "R", t0, 1))
	s.Add(makeEvent("ns", "uid-2", "R", t0.Add(time.Second), 1))
	s.Add(makeEvent("ns", "uid-3", "R", t0.Add(2*time.Second), 1))
	// ring is now full; adding uid-4 should evict uid-1
	s.Add(makeEvent("ns", "uid-4", "R", t0.Add(3*time.Second), 1))

	if s.Len() != 3 {
		t.Fatalf("expected 3 after eviction, got %d", s.Len())
	}

	// uid-1 should be gone from the namespace index
	results := s.Query(Filter{ObjectUID: "uid-1"})
	if len(results) != 0 {
		t.Errorf("expected evicted uid-1 to be absent, got %d results", len(results))
	}

	// uid-4 should be present
	results = s.Query(Filter{ObjectUID: "uid-4"})
	if len(results) != 1 {
		t.Errorf("expected uid-4 to be present, got %d results", len(results))
	}
}

func TestStore_QueryByNamespace(t *testing.T) {
	s := New(100)
	s.Add(makeEvent("payments", "uid-1", "BackOff", time.Now(), 1))
	s.Add(makeEvent("payments", "uid-2", "OOMKilled", time.Now(), 1))
	s.Add(makeEvent("monitoring", "uid-3", "BackOff", time.Now(), 1))

	got := s.Query(Filter{Namespace: "payments"})
	if len(got) != 2 {
		t.Fatalf("expected 2 events in payments, got %d", len(got))
	}
}

func TestStore_QueryByUID(t *testing.T) {
	s := New(100)
	s.Add(makeEvent("ns", "uid-1", "BackOff", time.Now(), 1))
	s.Add(makeEvent("ns", "uid-1", "OOMKilled", time.Now(), 1))
	s.Add(makeEvent("ns", "uid-2", "BackOff", time.Now(), 1))

	got := s.Query(Filter{ObjectUID: "uid-1"})
	if len(got) != 2 {
		t.Fatalf("expected 2 events for uid-1, got %d", len(got))
	}
}

func TestStore_QueryByReason(t *testing.T) {
	s := New(100)
	s.Add(makeEvent("ns", "uid-1", "BackOff", time.Now(), 1))
	s.Add(makeEvent("ns", "uid-2", "BackOff", time.Now(), 1))
	s.Add(makeEvent("ns", "uid-3", "OOMKilled", time.Now(), 1))

	got := s.Query(Filter{Reason: "BackOff"})
	if len(got) != 2 {
		t.Fatalf("expected 2 BackOff events, got %d", len(got))
	}
}

func TestStore_QuerySince(t *testing.T) {
	s := New(100)
	now := time.Unix(2000, 0)
	s.Add(makeEvent("ns", "uid-1", "R", now.Add(-2*time.Hour), 1))    // old
	s.Add(makeEvent("ns", "uid-2", "R", now.Add(-30*time.Minute), 1)) // recent
	s.Add(makeEvent("ns", "uid-3", "R", now, 1))                      // now

	got := s.Query(Filter{Since: now.Add(-time.Hour)})
	if len(got) != 2 {
		t.Fatalf("expected 2 recent events, got %d", len(got))
	}
}

func TestStore_QueryIntersection(t *testing.T) {
	s := New(100)
	s.Add(makeEvent("payments", "uid-1", "BackOff", time.Now(), 1))
	s.Add(makeEvent("payments", "uid-2", "OOMKilled", time.Now(), 1))
	s.Add(makeEvent("monitoring", "uid-3", "BackOff", time.Now(), 1))

	got := s.Query(Filter{Namespace: "payments", Reason: "BackOff"})
	if len(got) != 1 {
		t.Fatalf("expected 1 event (intersection), got %d", len(got))
	}
	if got[0].InvolvedObjectUID != "uid-1" {
		t.Errorf("unexpected event: %v", got[0])
	}
}

func TestStore_QueryLimit(t *testing.T) {
	s := New(100)
	now := time.Now()
	for i := 0; i < 10; i++ {
		s.Add(makeEvent("ns", fmt.Sprintf("uid-%d", i), "R", now.Add(time.Duration(i)*time.Second), 1))
	}

	got := s.Query(Filter{Limit: 3})
	if len(got) != 3 {
		t.Fatalf("expected 3 with limit, got %d", len(got))
	}
}

func TestStore_QuerySortedByLastSeenDesc(t *testing.T) {
	s := New(100)
	now := time.Unix(3000, 0)
	s.Add(makeEvent("ns", "uid-1", "R", now.Add(-2*time.Minute), 1))
	s.Add(makeEvent("ns", "uid-2", "R", now, 1))
	s.Add(makeEvent("ns", "uid-3", "R", now.Add(-time.Minute), 1))

	got := s.Query(Filter{})
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	if !got[0].LastSeen.Equal(now) {
		t.Errorf("first result should be most recent, got %v", got[0].LastSeen)
	}
}

func TestStore_UpsertUpdatesInPlace(t *testing.T) {
	s := New(100)
	now := time.Unix(1000, 0)

	s.Add(makeEvent("ns", "uid-1", "BackOff", now, 3))
	if s.Len() != 1 {
		t.Fatalf("expected 1, got %d", s.Len())
	}

	// Same uid+reason: should update in place, not insert a new entry.
	updated := makeEvent("ns", "uid-1", "BackOff", now.Add(time.Minute), 7)
	s.Add(updated)
	if s.Len() != 1 {
		t.Fatalf("expected 1 after upsert, got %d (duplicate inserted)", s.Len())
	}

	got := s.Query(Filter{ObjectUID: "uid-1"})
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].Count != 7 {
		t.Errorf("expected count 7 after upsert, got %d", got[0].Count)
	}
	if got[0].LastSeen != now.Add(time.Minute) {
		t.Error("expected LastSeen to be updated")
	}
}

func TestStore_BurstEpisodesKeptSeparately(t *testing.T) {
	s := New(100)
	now := time.Unix(5000, 0)

	// First burst: BackOff, count climbs 1→5
	s.Add(makeEvent("ns", "uid-1", "BackOff", now, 1))
	s.Add(makeEvent("ns", "uid-1", "BackOff", now.Add(time.Minute), 5))
	if s.Len() != 1 {
		t.Fatalf("expected 1 entry after in-burst updates, got %d", s.Len())
	}

	// Second burst: count resets to 1 after a gap → new episode
	later := now.Add(10 * time.Minute)
	e2 := makeEvent("ns", "uid-1", "BackOff", later, 1)
	e2.FirstSeen = later
	s.Add(e2)
	if s.Len() != 2 {
		t.Fatalf("expected 2 entries after second burst, got %d", s.Len())
	}

	got := s.Query(Filter{ObjectUID: "uid-1", Reason: "BackOff"})
	if len(got) != 2 {
		t.Fatalf("expected 2 burst episodes in query, got %d", len(got))
	}
}

func TestStore_BurstCapEnforced(t *testing.T) {
	s := New(100)
	now := time.Unix(6000, 0)

	// Add maxBurstsPerKey+2 separate burst episodes (count resets each time).
	for i := 0; i < maxBurstsPerKey+2; i++ {
		t0 := now.Add(time.Duration(i) * 10 * time.Minute)
		e := makeEvent("ns", "uid-x", "BackOff", t0, 1)
		e.FirstSeen = t0
		s.Add(e)
	}

	got := s.Query(Filter{ObjectUID: "uid-x", Reason: "BackOff"})
	if len(got) > maxBurstsPerKey+2 {
		t.Errorf("expected at most %d burst episodes tracked, got %d", maxBurstsPerKey+2, len(got))
	}
	// History slice must not exceed cap.
	key := dedupKey{objectUID: "uid-x", reason: "BackOff"}
	s.mu.RLock()
	histLen := len(s.dedupIdx[key])
	s.mu.RUnlock()
	if histLen > maxBurstsPerKey {
		t.Errorf("dedup history len %d exceeds cap %d", histLen, maxBurstsPerKey)
	}
}

func TestStore_SummarizeNamespace(t *testing.T) {
	s := New(100)
	now := time.Now()
	s.Add(makeEvent("ns", "uid-1", "BackOff", now, 5))
	s.Add(makeEvent("ns", "uid-2", "BackOff", now.Add(time.Second), 3))
	s.Add(makeEvent("ns", "uid-3", "OOMKilled", now, 1))

	summary := s.SummarizeNamespace("ns", time.Time{})
	if len(summary) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(summary))
	}

	for _, g := range summary {
		if g.Reason == "BackOff" && g.TotalCount != 8 {
			t.Errorf("BackOff: expected count 8, got %d", g.TotalCount)
		}
	}
}

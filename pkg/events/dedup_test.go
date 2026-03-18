package events

import (
	"testing"
	"time"
)

func TestDeduplicator_NewEvent(t *testing.T) {
	d := NewDeduplicator()
	e := NormalizedEvent{
		InvolvedObjectUID: "uid-1",
		Reason:            "BackOff",
		Count:             3,
		FirstSeen:         time.Unix(100, 0),
		LastSeen:          time.Unix(200, 0),
		Message:           "back-off restarting",
	}

	merged, isNew := d.Merge(e)
	if !isNew {
		t.Fatal("expected isNew=true for first insertion")
	}
	if merged.Count != 3 {
		t.Fatalf("expected count 3, got %d", merged.Count)
	}
}

func TestDeduplicator_MergeAccumulatesCount(t *testing.T) {
	d := NewDeduplicator()
	base := NormalizedEvent{
		InvolvedObjectUID: "uid-1",
		Reason:            "BackOff",
		Count:             3,
		FirstSeen:         time.Unix(100, 0),
		LastSeen:          time.Unix(200, 0),
		Message:           "back-off restarting",
	}
	d.Merge(base)

	update := base
	update.Count = 5
	update.LastSeen = time.Unix(300, 0)
	update.Message = "back-off restarting failed"

	merged, isNew := d.Merge(update)
	if isNew {
		t.Fatal("expected isNew=false on second merge")
	}
	if merged.Count != 8 {
		t.Fatalf("expected count 8, got %d", merged.Count)
	}
	if merged.LastSeen != time.Unix(300, 0) {
		t.Fatal("expected lastSeen to be updated")
	}
	if merged.FirstSeen != time.Unix(100, 0) {
		t.Fatal("expected firstSeen to be preserved")
	}
}

func TestDeduplicator_DifferentReasonsDontMerge(t *testing.T) {
	d := NewDeduplicator()
	e1 := NormalizedEvent{InvolvedObjectUID: "uid-1", Reason: "BackOff", Count: 1, FirstSeen: time.Unix(1, 0), LastSeen: time.Unix(1, 0)}
	e2 := NormalizedEvent{InvolvedObjectUID: "uid-1", Reason: "OOMKilled", Count: 1, FirstSeen: time.Unix(2, 0), LastSeen: time.Unix(2, 0)}
	d.Merge(e1)
	d.Merge(e2)

	snap := d.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 distinct events, got %d", len(snap))
	}
}

func TestNormalizeMessage(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  hello   world  ", "hello world"},
		{"already clean", "already clean"},
		{"  ", ""},
	}
	for _, c := range cases {
		got := normalizeMessage(c.in)
		if got != c.want {
			t.Errorf("normalizeMessage(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

package events

import "sync"

// dedupKey identifies a unique event stream: same object + same reason.
type dedupKey struct {
	objectUID string
	reason    string
}

// Deduplicator merges incoming NormalizedEvents by (objectUID, reason),
// accumulating count and tracking firstSeen/lastSeen.
type Deduplicator struct {
	mu    sync.Mutex
	index map[dedupKey]*NormalizedEvent
}

func NewDeduplicator() *Deduplicator {
	return &Deduplicator{index: make(map[dedupKey]*NormalizedEvent)}
}

// Merge upserts the event into the dedup index.
// Returns the merged event and whether it was newly inserted (true) or updated (false).
func (d *Deduplicator) Merge(e NormalizedEvent) (NormalizedEvent, bool) {
	key := dedupKey{objectUID: e.InvolvedObjectUID, reason: e.Reason}

	d.mu.Lock()
	defer d.mu.Unlock()

	existing, ok := d.index[key]
	if !ok {
		copy := e
		d.index[key] = &copy
		return copy, true
	}

	existing.Count += e.Count
	if e.FirstSeen.Before(existing.FirstSeen) {
		existing.FirstSeen = e.FirstSeen
	}
	if e.LastSeen.After(existing.LastSeen) {
		existing.LastSeen = e.LastSeen
		existing.Message = e.Message // keep most recent message
	}
	// Preserve enriched owner chain if already set.
	if len(existing.OwnerChain) == 0 && len(e.OwnerChain) > 0 {
		existing.OwnerChain = e.OwnerChain
	}

	return *existing, false
}

// Get returns the current merged state for a given object UID and reason.
func (d *Deduplicator) Get(objectUID, reason string) (NormalizedEvent, bool) {
	key := dedupKey{objectUID: objectUID, reason: reason}
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.index[key]
	if !ok {
		return NormalizedEvent{}, false
	}
	return *e, true
}

// Snapshot returns all current deduplicated events.
func (d *Deduplicator) Snapshot() []NormalizedEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]NormalizedEvent, 0, len(d.index))
	for _, e := range d.index {
		out = append(out, *e)
	}
	return out
}

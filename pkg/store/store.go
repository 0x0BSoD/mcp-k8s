package store

import (
	"sync"
	"time"

	"github.com/0x0BSoD/mcp-k8s/pkg/events"
)

const DefaultMaxSize = 10_000

type dedupKey struct {
	objectUID string
	reason    string
}

// Store is a thread-safe, fixed-capacity ring buffer of NormalizedEvents
// with inverted indexes on namespace, object UID, and reason.
// Events with the same (InvolvedObjectUID, Reason) pair are upserted in place.
type Store struct {
	mu      sync.RWMutex
	maxSize int

	// ring buffer: order holds event IDs in insertion order
	order  []uint64
	head   int // next write slot
	full   bool
	nextID uint64

	// primary storage
	byID map[uint64]*events.NormalizedEvent

	// dedup: (objectUID, reason) → event ID — enables upsert semantics
	dedupIdx map[dedupKey]uint64

	// inverted indexes: field value → set of event IDs
	byNamespace map[string]map[uint64]struct{}
	byUID       map[string]map[uint64]struct{}
	byReason    map[string]map[uint64]struct{}
}

func New(maxSize int) *Store {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	return &Store{
		maxSize:     maxSize,
		order:       make([]uint64, maxSize),
		byID:        make(map[uint64]*events.NormalizedEvent),
		dedupIdx:    make(map[dedupKey]uint64),
		byNamespace: make(map[string]map[uint64]struct{}),
		byUID:       make(map[string]map[uint64]struct{}),
		byReason:    make(map[string]map[uint64]struct{}),
	}
}

// Add inserts or updates a NormalizedEvent.
// If an event with the same (InvolvedObjectUID, Reason) already exists it is
// updated in place (count, timestamps, message, owner chain).  Otherwise the
// event is appended to the ring; when the ring is full the oldest slot is evicted.
func (s *Store) Add(e events.NormalizedEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Upsert path: same (uid, reason) → update in place.
	if e.InvolvedObjectUID != "" {
		key := dedupKey{objectUID: e.InvolvedObjectUID, reason: e.Reason}
		if existingID, ok := s.dedupIdx[key]; ok {
			ex := s.byID[existingID]
			ex.Count = e.Count // k8s event count is always the running total
			if e.LastSeen.After(ex.LastSeen) {
				ex.LastSeen = e.LastSeen
				ex.Message = e.Message
			}
			if !e.FirstSeen.IsZero() && (ex.FirstSeen.IsZero() || e.FirstSeen.Before(ex.FirstSeen)) {
				ex.FirstSeen = e.FirstSeen
			}
			if len(e.OwnerChain) > 0 {
				ex.OwnerChain = e.OwnerChain
			}
			return
		}
	}

	// Insert path.
	if s.full {
		s.evict(s.order[s.head])
	}

	id := s.nextID
	s.nextID++

	cp := e
	s.byID[id] = &cp
	s.order[s.head] = id
	s.head = (s.head + 1) % s.maxSize
	if s.head == 0 {
		s.full = true
	}

	if e.InvolvedObjectUID != "" {
		s.dedupIdx[dedupKey{objectUID: e.InvolvedObjectUID, reason: e.Reason}] = id
	}
	addToIndex(s.byNamespace, e.Namespace, id)
	addToIndex(s.byUID, e.InvolvedObjectUID, id)
	addToIndex(s.byReason, e.Reason, id)
}

// Len returns the number of events currently stored.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID)
}

func (s *Store) evict(id uint64) {
	e, ok := s.byID[id]
	if !ok {
		return
	}
	if e.InvolvedObjectUID != "" {
		delete(s.dedupIdx, dedupKey{objectUID: e.InvolvedObjectUID, reason: e.Reason})
	}
	removeFromIndex(s.byNamespace, e.Namespace, id)
	removeFromIndex(s.byUID, e.InvolvedObjectUID, id)
	removeFromIndex(s.byReason, e.Reason, id)
	delete(s.byID, id)
}

func addToIndex(idx map[string]map[uint64]struct{}, key string, id uint64) {
	if key == "" {
		return
	}
	if idx[key] == nil {
		idx[key] = make(map[uint64]struct{})
	}
	idx[key][id] = struct{}{}
}

func removeFromIndex(idx map[string]map[uint64]struct{}, key string, id uint64) {
	if s, ok := idx[key]; ok {
		delete(s, id)
		if len(s) == 0 {
			delete(idx, key)
		}
	}
}

// Query returns events matching the given filter, sorted by LastSeen descending.
func (s *Store) Query(f Filter) []events.NormalizedEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	candidates := s.candidateIDs(f)

	var out []events.NormalizedEvent
	for id := range candidates {
		e := s.byID[id]
		if e == nil {
			continue
		}
		if !f.Since.IsZero() && e.LastSeen.Before(f.Since) {
			continue
		}
		out = append(out, *e)
	}

	sortByLastSeenDesc(out)

	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out
}

// candidateIDs returns the intersection of index sets for any non-empty filter fields.
// If no indexed filter is set, all IDs are candidates.
func (s *Store) candidateIDs(f Filter) map[uint64]struct{} {
	var sets []map[uint64]struct{}

	if f.Namespace != "" {
		sets = append(sets, s.byNamespace[f.Namespace])
	}
	if f.ObjectUID != "" {
		sets = append(sets, s.byUID[f.ObjectUID])
	}
	if f.Reason != "" {
		sets = append(sets, s.byReason[f.Reason])
	}

	if len(sets) == 0 {
		all := make(map[uint64]struct{}, len(s.byID))
		for id := range s.byID {
			all[id] = struct{}{}
		}
		return all
	}

	return intersect(sets)
}

func intersect(sets []map[uint64]struct{}) map[uint64]struct{} {
	if len(sets) == 0 {
		return nil
	}
	smallest := sets[0]
	for _, s := range sets[1:] {
		if len(s) < len(smallest) {
			smallest = s
		}
	}

	result := make(map[uint64]struct{})
	for id := range smallest {
		inAll := true
		for _, s := range sets {
			if _, ok := s[id]; !ok {
				inAll = false
				break
			}
		}
		if inAll {
			result[id] = struct{}{}
		}
	}
	return result
}

func sortByLastSeenDesc(evs []events.NormalizedEvent) {
	for i := 1; i < len(evs); i++ {
		for j := i; j > 0 && evs[j].LastSeen.After(evs[j-1].LastSeen); j-- {
			evs[j], evs[j-1] = evs[j-1], evs[j]
		}
	}
}

// NamespaceSummary groups events in a namespace by reason.
type NamespaceSummary struct {
	Reason     string
	TotalCount int32
	Objects    []string
	LastSeen   time.Time
}

// SummarizeNamespace returns per-reason groups for all events in the namespace
// observed after `since` (zero = no lower bound).
func (s *Store) SummarizeNamespace(namespace string, since time.Time) []NamespaceSummary {
	evs := s.Query(Filter{Namespace: namespace, Since: since})

	type group struct {
		count    int32
		objects  map[string]struct{}
		lastSeen time.Time
	}
	groups := make(map[string]*group)

	for _, e := range evs {
		g, ok := groups[e.Reason]
		if !ok {
			g = &group{objects: make(map[string]struct{})}
			groups[e.Reason] = g
		}
		g.count += e.Count
		g.objects[e.InvolvedObjectName] = struct{}{}
		if e.LastSeen.After(g.lastSeen) {
			g.lastSeen = e.LastSeen
		}
	}

	out := make([]NamespaceSummary, 0, len(groups))
	for reason, g := range groups {
		objs := make([]string, 0, len(g.objects))
		for o := range g.objects {
			objs = append(objs, o)
		}
		out = append(out, NamespaceSummary{
			Reason:     reason,
			TotalCount: g.count,
			Objects:    objs,
			LastSeen:   g.lastSeen,
		})
	}
	return out
}

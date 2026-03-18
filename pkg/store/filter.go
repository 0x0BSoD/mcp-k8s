package store

import "time"

// Filter defines query parameters for Store.Query.
// Zero values mean "no constraint" for that field.
type Filter struct {
	Namespace string
	ObjectUID string
	Reason    string
	Since     time.Time // inclusive lower bound on LastSeen
	Limit     int       // 0 = unlimited
}

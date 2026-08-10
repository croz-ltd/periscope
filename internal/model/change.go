package model

import "time"

// Change kinds. Component-level kinds carry a component key; cluster-level ones
// describe the cluster itself.
const (
	ChangeAdded       = "added"       // component appeared on this cluster
	ChangeRemoved     = "removed"     // component disappeared from this cluster
	ChangeUpdated     = "updated"     // component's value changed
	ChangeJoined      = "joined"      // first successful scrape of this cluster
	ChangeUnreachable = "unreachable" // cluster stopped answering
	ChangeRecovered   = "recovered"   // cluster answered again
)

// Change is one difference between a cluster's snapshot and the one before it.
// Changes are recorded as snapshots are saved, so a scrape that found nothing
// new records nothing at all: the feed only ever holds real events.
type Change struct {
	Time    time.Time `json:"time"`
	Cluster string    `json:"cluster"`
	Kind    string    `json:"kind"`              // see Change* consts
	Key     string    `json:"key,omitempty"`     // component key, empty for cluster-level kinds
	Name    string    `json:"name,omitempty"`    // component display name at the time
	Group   string    `json:"group,omitempty"`   // component display group
	Compare string    `json:"compare,omitempty"` // component comparison kind, so counters can be filtered out
	From    string    `json:"from,omitempty"`    // previous value
	To      string    `json:"to,omitempty"`      // new value
}

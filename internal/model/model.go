// Package model holds the data types shared across extraction, storage, and the API.
package model

import "time"

// Component is one version-bearing thing found on a cluster: the OpenShift
// release, an operator (from its CSV), a managed CSI driver, or the node fleet.
type Component struct {
	Key       string            `json:"key"`  // stable matrix-row identity (e.g. "openshift", OLM package)
	Name      string            `json:"name"` // human display name
	Kind      string            `json:"kind"` // openshift | operator | csi | nodes
	Version   string            `json:"version"`
	Namespace string            `json:"namespace,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// Snapshot is the result of scraping one cluster at one moment. OK is false only
// when the cluster was unreachable / auth failed; per-extractor failures leave
// OK true but populate Error, and successfully-read components still land here.
type Snapshot struct {
	Cluster    string      `json:"cluster"`
	Time       time.Time   `json:"time"`
	OK         bool        `json:"ok"`
	Error      string      `json:"error,omitempty"`
	Components []Component `json:"components"`
}

// Package model holds the data types shared across extraction, storage, and the API.
package model

import "time"

// Display groups (matrix sections).
const (
	GroupOpenShift = "OpenShift"
	GroupNode      = "Node"
	GroupOperators = "Operators"
	GroupCert      = "Certificate"
	GroupVirt      = "OpenShift Virtualization"
)

// Comparison kinds — how a row's cells are judged.
const (
	// CompareVersion: semver drift vs the fleet-max (green ahead / red behind).
	CompareVersion = "version"
	// CompareMatch: config consistency — cells that differ from the fleet's
	// common value are flagged; no "leader".
	CompareMatch = "match"
	// CompareExpiry: absolute date thresholds, NOT cross-cluster (Version holds
	// an RFC3339 timestamp): >120d green, <=120d yellow, <=60d red.
	CompareExpiry = "expiry"
)

// Component is one comparable fact found on a cluster: a version, a config
// value, or a certificate expiry.
type Component struct {
	Key       string            `json:"key"`     // stable matrix-row identity
	Name      string            `json:"name"`    // human display name
	Group     string            `json:"group"`   // display group (see Group* consts)
	Compare   string            `json:"compare"` // comparison kind (see Compare* consts); "" == version
	Kind      string            `json:"kind"`    // semantic kind: openshift | operator | csi | nodes | cert | virt
	Version   string            `json:"version"` // the value (version / config string / RFC3339 expiry)
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
	Order      int         `json:"order"` // column order (lower = left); from the Secret's order label
	Components []Component `json:"components"`
}

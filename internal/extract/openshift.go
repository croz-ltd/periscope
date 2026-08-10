package extract

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/version"
)

// OpenShift reads the cluster release from the ClusterVersion "version" object.
type OpenShift struct{}

func (OpenShift) Key() string { return "openshift" }

var clusterVersionGVR = schema.GroupVersionResource{
	Group: "config.openshift.io", Version: "v1", Resource: "clusterversions",
}

func (OpenShift) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	obj, err := c.Dynamic.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	ver, _, _ := unstructured.NestedString(obj.Object, "status", "desired", "version")
	channel, _, _ := unstructured.NestedString(obj.Object, "spec", "channel")

	comps := []model.Component{{
		Key:     "openshift",
		Name:    "OpenShift",
		Group:   model.GroupOpenShift,
		Compare: model.CompareVersion,
		Kind:    "openshift",
		Version: ver,
	}}
	// Update channel: a config value compared for consistency across the fleet.
	comps = append(comps, model.Component{
		Key:     "openshift-channel",
		Name:    "Update channel",
		Group:   model.GroupOpenShift,
		Compare: model.CompareMatch,
		Kind:    "openshift",
		Version: channel,
	})
	return append(comps, updateStatus(obj.Object)...), nil
}

// updateStatus turns the ClusterVersion's own upgrade view into rows: what the
// cluster could move to, and whether it is allowed to move at all.
//
// The cluster has already asked the update service on its own channel, so this
// needs no egress from the hub and no knowledge of Cincinnati here. It answers
// the question the drift matrix cannot: being behind the fleet only matters if
// there is somewhere to go, and a cluster that is Upgradeable=False is stuck
// until someone clears the reason, however green its row looks.
func updateStatus(obj map[string]any) []model.Component {
	var out []model.Component

	if latest := newestAvailableUpdate(obj); latest != "" {
		out = append(out, model.Component{
			Key:     "openshift-update-available",
			Name:    "Update available",
			Group:   model.GroupOpenShift,
			Compare: model.CompareInfo,
			Kind:    "openshift",
			Version: latest,
		})
	}

	// Upgradeable=False names an operator or configuration blocking the next
	// minor upgrade, and its message is the actual to-do item.
	if cond, ok := condition(obj, "Upgradeable"); ok && cond["status"] == "False" {
		extra := map[string]string{}
		if msg, _ := cond["message"].(string); msg != "" {
			extra["message"] = msg
		}
		reason, _ := cond["reason"].(string)
		if reason == "" {
			reason = "Blocked"
		}
		out = append(out, model.Component{
			Key:     "openshift-upgradeable",
			Name:    "Upgrade blocked",
			Group:   model.GroupOpenShift,
			Compare: model.CompareMatch,
			Kind:    "openshift",
			Version: reason,
			Extra:   extra,
		})
	}

	// Progressing=True means an upgrade is running right now, which explains a
	// version that disagrees with the rest of the fleet for the next hour.
	if cond, ok := condition(obj, "Progressing"); ok && cond["status"] == "True" {
		msg, _ := cond["message"].(string)
		out = append(out, model.Component{
			Key:     "openshift-updating",
			Name:    "Update in progress",
			Group:   model.GroupOpenShift,
			Compare: model.CompareInfo,
			Kind:    "openshift",
			Version: "yes",
			Extra:   map[string]string{"message": msg},
		})
	}
	return out
}

// newestAvailableUpdate returns the highest version the cluster is being
// offered, or "" when it is already at the head of its channel.
func newestAvailableUpdate(obj map[string]any) string {
	updates, _, _ := unstructured.NestedSlice(obj, "status", "availableUpdates")
	best := version.Version{}
	newest := ""
	for _, u := range updates {
		m, ok := u.(map[string]any)
		if !ok {
			continue
		}
		v, _ := m["version"].(string)
		if v == "" {
			continue
		}
		parsed := version.Parse(v)
		if !parsed.OK {
			continue
		}
		if newest == "" || version.Compare(parsed, best) > 0 {
			best, newest = parsed, v
		}
	}
	return newest
}

// condition returns the named ClusterVersion status condition.
func condition(obj map[string]any, want string) (map[string]any, bool) {
	conds, _, _ := unstructured.NestedSlice(obj, "status", "conditions")
	for _, c := range conds {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := m["type"].(string); t == want {
			return m, true
		}
	}
	return nil, false
}

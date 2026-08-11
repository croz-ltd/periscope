package extract

import (
	"context"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/periscope/internal/model"
)

// OLM enumerates every installed operator via its ClusterServiceVersion. Rows
// are keyed by the stable OLM package name (resolved from Subscriptions) so the
// same operator lines up across clusters even as its version changes.
type OLM struct{}

func (OLM) Key() string { return "olm" }

var (
	csvGVR         = schema.GroupVersionResource{Group: "operators.coreos.com", Version: "v1alpha1", Resource: "clusterserviceversions"}
	subGVR         = schema.GroupVersionResource{Group: "operators.coreos.com", Version: "v1alpha1", Resource: "subscriptions"}
	installPlanGVR = schema.GroupVersionResource{Group: "operators.coreos.com", Version: "v1alpha1", Resource: "installplans"}
)

// pendingUpdate is an operator OLM has a newer version for but has not applied.
type pendingUpdate struct {
	pkg      string // package name, the operator's row key
	from, to string // installed CSV -> the CSV OLM wants to move to
}

func (OLM) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	// Map installedCSV -> package name for stable row keys (best-effort).
	pkgByCSV := map[string]string{}
	var pending []pendingUpdate
	if subs, err := c.Dynamic.Resource(subGVR).List(ctx, metav1.ListOptions{}); err == nil {
		for _, s := range subs.Items {
			pkg, _, _ := unstructured.NestedString(s.Object, "spec", "name")
			installed, _, _ := unstructured.NestedString(s.Object, "status", "installedCSV")
			if pkg != "" && installed != "" {
				pkgByCSV[installed] = pkg
			}
			// currentCSV is what the channel resolves to now. It differing from
			// installedCSV means the update exists and has not been applied,
			// which on manual approval can sit unnoticed for months.
			current, _, _ := unstructured.NestedString(s.Object, "status", "currentCSV")
			if pkg != "" && current != "" && installed != "" && current != installed {
				pending = append(pending, pendingUpdate{pkg: pkg, from: installed, to: current})
			}
		}
	}

	// OLM copies each CSV into every watched namespace. Copies carry the
	// "olm.copiedFrom" label, and CSV objects are large (embedded install strategy
	// + icons). Excluding copies keeps us to one object per actual install and
	// avoids listing thousands of duplicates (which OOMs the pod on real clusters).
	list, err := c.Dynamic.Resource(csvGVR).List(ctx, metav1.ListOptions{LabelSelector: "!olm.copiedFrom"})
	if err != nil {
		return nil, err
	}

	pendingByPkg := map[string]pendingUpdate{}
	for _, p := range pending {
		pendingByPkg[p.pkg] = p
	}

	seen := map[string]bool{} // an operator installed in several namespaces => dedupe originals by CSV name
	var out []model.Component
	for _, item := range list.Items {
		name := item.GetName()
		if seen[name] {
			continue
		}
		seen[name] = true

		ver, _, _ := unstructured.NestedString(item.Object, "spec", "version")
		display, _, _ := unstructured.NestedString(item.Object, "spec", "displayName")
		key := pkgByCSV[name]
		if key == "" {
			key = stripVersionSuffix(name)
		}
		if display == "" {
			display = key
		}
		comp := model.Component{
			Key:       key,
			Name:      display,
			Group:     model.GroupOperators,
			Compare:   model.CompareVersion,
			Kind:      "operator",
			Version:   ver,
			Namespace: item.GetNamespace(),
		}
		// An operator with an update waiting carries it on its own cell, so the
		// answer is where you are already looking rather than in a summary row.
		if p, ok := pendingByPkg[key]; ok {
			comp.Extra = map[string]string{"updatePending": p.to}
		}
		out = append(out, comp)
	}
	return append(out, updateBacklog(ctx, c, pending)...), nil
}

// updateBacklog summarises what OLM is waiting to do on this cluster: updates
// resolved but not applied, and InstallPlans sitting unapproved.
//
// Both are counted rather than compared, because they are a property of one
// cluster's maintenance state, not a fleet-wide value. They are the reason a
// cluster silently falls behind: nothing is broken, nobody clicked approve.
func updateBacklog(ctx context.Context, c *Clients, pending []pendingUpdate) []model.Component {
	out := []model.Component{{
		Key:     "olm-updates-pending",
		Name:    "Operator updates pending",
		Group:   model.GroupOperators,
		Compare: model.CompareInfo,
		Kind:    "operator",
		Version: strconv.Itoa(len(pending)),
		Extra:   pendingDetail(pending),
	}}

	if !c.HasResource(installPlanGVR) {
		return out
	}
	plans, err := c.Dynamic.Resource(installPlanGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return out
	}
	waiting := map[string]string{}
	for _, p := range plans.Items {
		approved, _, _ := unstructured.NestedBool(p.Object, "spec", "approved")
		approval, _, _ := unstructured.NestedString(p.Object, "spec", "approval")
		phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
		if approved || approval != "Manual" || phase == "Complete" {
			continue
		}
		csvs, _, _ := unstructured.NestedStringSlice(p.Object, "spec", "clusterServiceVersionNames")
		waiting[p.GetNamespace()+"/"+p.GetName()] = strings.Join(csvs, ", ")
	}
	return append(out, model.Component{
		Key:     "olm-installplans-waiting",
		Name:    "InstallPlans awaiting approval",
		Group:   model.GroupOperators,
		Compare: model.CompareInfo,
		Kind:    "operator",
		Version: strconv.Itoa(len(waiting)),
		Extra:   waiting,
	})
}

// pendingDetail lists the waiting updates as "package: from -> to" for the
// cell's tooltip, so the count is actionable without another query.
func pendingDetail(pending []pendingUpdate) map[string]string {
	if len(pending) == 0 {
		return nil
	}
	detail := make(map[string]string, len(pending))
	for _, p := range pending {
		detail[p.pkg] = p.from + " -> " + p.to
	}
	return detail
}

// stripVersionSuffix turns "portworx-operator.v3.1.0" into "portworx-operator".
func stripVersionSuffix(csvName string) string {
	if i := strings.Index(csvName, ".v"); i > 0 {
		return csvName[:i]
	}
	return csvName
}

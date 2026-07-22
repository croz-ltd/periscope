package extract

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/version"
)

// Nodes reports the kubelet version of the fleet. When nodes disagree (mid-
// upgrade skew) it reports the highest version and records the distribution.
type Nodes struct{}

func (Nodes) Key() string { return "nodes" }

func (Nodes) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	nodes, err := c.Typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	roles := map[string]int{}
	for _, n := range nodes.Items {
		counts[n.Status.NodeInfo.KubeletVersion]++
		for label := range n.Labels {
			if r, ok := strings.CutPrefix(label, "node-role.kubernetes.io/"); ok && r != "" {
				roles[r]++
			}
		}
	}
	if len(nodes.Items) == 0 {
		return nil, nil
	}

	// Total node count — informational (clusters legitimately differ in size).
	total := model.Component{
		Key: "node-count", Name: "Total nodes", Group: model.GroupNode,
		Compare: model.CompareInfo, Kind: "nodes",
		Version: strconv.Itoa(len(nodes.Items)),
		Extra:   map[string]string{},
	}
	roleNames := make([]string, 0, len(roles))
	for r := range roles {
		roleNames = append(roleNames, r)
	}
	sort.Strings(roleNames)
	for _, r := range roleNames {
		total.Extra[r] = strconv.Itoa(roles[r])
	}

	if len(counts) == 0 {
		return []model.Component{total}, nil
	}

	versions := make([]string, 0, len(counts))
	for v := range counts {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return version.Compare(version.Parse(versions[i]), version.Parse(versions[j])) < 0
	})
	top := versions[len(versions)-1]

	extra := map[string]string{}
	if len(counts) > 1 {
		extra["skew"] = "true"
		var b []byte
		for _, v := range versions {
			b = append(b, fmt.Sprintf("%s×%d ", v, counts[v])...)
		}
		extra["distribution"] = string(b)
	}
	kubelet := model.Component{
		Key:     "node-kubelet",
		Name:    "Kubelet",
		Group:   model.GroupNode,
		Compare: model.CompareVersion,
		Kind:    "nodes",
		Version: top,
		Extra:   extra,
	}
	return []model.Component{total, kubelet}, nil
}

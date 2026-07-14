package extract

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/croz-ltd/cluster-comparator/internal/model"
	"github.com/croz-ltd/cluster-comparator/internal/version"
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
	for _, n := range nodes.Items {
		counts[n.Status.NodeInfo.KubeletVersion]++
	}
	if len(counts) == 0 {
		return nil, nil
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
	return []model.Component{{
		Key:     "node-kubelet",
		Name:    "Kubelet",
		Kind:    "nodes",
		Version: top,
		Extra:   extra,
	}}, nil
}

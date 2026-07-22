package extract

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/croz-ltd/periscope/internal/model"
)

// StorageVolumes counts PersistentVolumeClaims and PersistentVolumes per storage
// class. Counts are informational (they differ per cluster by design).
type StorageVolumes struct{}

func (StorageVolumes) Key() string { return "storage-volumes" }

func (StorageVolumes) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	pvcByClass := map[string]int{}
	pvcs, err := c.Typed.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, p := range pvcs.Items {
		pvcByClass[scOrNone(p.Spec.StorageClassName)]++
	}

	pvByClass := map[string]int{}
	pvs, err := c.Typed.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, p := range pvs.Items {
		sc := p.Spec.StorageClassName
		if sc == "" {
			sc = "(none)"
		}
		pvByClass[sc]++
	}

	classes := map[string]bool{}
	for k := range pvcByClass {
		classes[k] = true
	}
	for k := range pvByClass {
		classes[k] = true
	}
	names := make([]string, 0, len(classes))
	for k := range classes {
		names = append(names, k)
	}
	sort.Strings(names)

	totalPVC, totalPV := 0, 0
	var out []model.Component
	for _, sc := range names {
		pvc, pv := pvcByClass[sc], pvByClass[sc]
		totalPVC += pvc
		totalPV += pv
		out = append(out, model.Component{
			Key: "storage-" + sc, Name: "Storage: " + sc, Group: model.GroupStorage,
			Compare: model.CompareInfo, Kind: "storage",
			Version: fmt.Sprintf("%d PVC / %d PV", pvc, pv),
			Extra:   map[string]string{"pvc": strconv.Itoa(pvc), "pv": strconv.Itoa(pv)},
		})
	}
	total := model.Component{
		Key: "storage-total", Name: "Storage (all classes)", Group: model.GroupStorage,
		Compare: model.CompareInfo, Kind: "storage",
		Version: fmt.Sprintf("%d PVC / %d PV", totalPVC, totalPV),
	}
	return append([]model.Component{total}, out...), nil
}

// scOrNone maps a PVC's *string storage class to a display value.
func scOrNone(s *string) string {
	if s == nil || *s == "" {
		return "(none)"
	}
	return *s
}

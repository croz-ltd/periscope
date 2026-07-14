package extract

import (
	"context"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/croz-ltd/cluster-comparator/internal/model"
)

// DefaultStorageClass reports which StorageClass is marked default — a common
// source of cross-cluster config drift.
type DefaultStorageClass struct{}

func (DefaultStorageClass) Key() string { return "default-storageclass" }

const (
	defaultSCAnnotation     = "storageclass.kubernetes.io/is-default-class"
	defaultSCAnnotationBeta = "storageclass.beta.kubernetes.io/is-default-class"
)

func (DefaultStorageClass) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	list, err := c.Typed.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var defaults []string
	for _, sc := range list.Items {
		a := sc.Annotations
		if a[defaultSCAnnotation] == "true" || a[defaultSCAnnotationBeta] == "true" {
			defaults = append(defaults, sc.Name)
		}
	}
	sort.Strings(defaults)
	value := strings.Join(defaults, ", ") // "" when none, "a, b" if misconfigured with >1
	return []model.Component{{
		Key:     "default-storageclass",
		Name:    "Default StorageClass",
		Group:   model.GroupOpenShift,
		Compare: model.CompareMatch,
		Kind:    "openshift",
		Version: value,
	}}, nil
}

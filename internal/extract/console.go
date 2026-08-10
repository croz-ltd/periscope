package extract

import (
	"context"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/periscope/internal/model"
)

// ConsoleBanner reads the ConsoleNotification named "cluster-name", the banner
// many teams already pin to the top of the OpenShift console to say which
// cluster you are looking at ("PRODUCTION", in red).
//
// Reusing it means the matrix heads each column the way the cluster's own
// operators label it, colours included, instead of with whatever the joining
// Secret happened to be called. It is also self-maintaining: rename the banner
// on the cluster and the dashboard follows, with no hub-side edit.
type ConsoleBanner struct{}

func (ConsoleBanner) Key() string { return "console-banner" }

// bannerName is the ConsoleNotification this looks for. Anything else on the
// cluster is someone else's banner (maintenance notices and the like).
const bannerName = "cluster-name"

var consoleNotificationGVR = schema.GroupVersionResource{
	Group: "console.openshift.io", Version: "v1", Resource: "consolenotifications",
}

func (ConsoleBanner) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	if !c.HasResource(consoleNotificationGVR) {
		return nil, nil // not an OpenShift cluster, or console disabled
	}
	obj, err := c.Dynamic.Resource(consoleNotificationGVR).Get(ctx, bannerName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil // no banner: the column keeps the joined name
		}
		return nil, err
	}

	text, _, _ := unstructured.NestedString(obj.Object, "spec", "text")
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	color, _, _ := unstructured.NestedString(obj.Object, "spec", "color")
	background, _, _ := unstructured.NestedString(obj.Object, "spec", "backgroundColor")

	return []model.Component{{
		Key:     model.KeyClusterBanner,
		Name:    "Console banner",
		Group:   model.GroupOpenShift,
		Compare: model.CompareInfo,
		Kind:    "openshift",
		Version: strings.TrimSpace(text),
		Extra: map[string]string{
			"color":           color,
			"backgroundColor": background,
		},
	}}, nil
}

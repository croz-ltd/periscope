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

// ConsoleURL reads the cluster's own web console address from the
// Console config object, the same place `oc whoami --show-console` reads.
//
// The hub already knows every cluster's API endpoint, but the API endpoint is
// not somewhere a person can click to. Carrying the console URL turns each
// matrix column into a way in: see the drift, open the cluster that has it.
// Reading it from the cluster rather than deriving it from the API URL keeps
// custom console routes and non-default apps domains working.
type ConsoleURL struct{}

func (ConsoleURL) Key() string { return "console-url" }

var consoleConfigGVR = schema.GroupVersionResource{
	Group: "config.openshift.io", Version: "v1", Resource: "consoles",
}

// consoleConfigName is the singleton Console config object every OpenShift
// cluster carries.
const consoleConfigName = "cluster"

func (ConsoleURL) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	if !c.HasResource(consoleConfigGVR) {
		return nil, nil // not an OpenShift cluster
	}
	obj, err := c.Dynamic.Resource(consoleConfigGVR).Get(ctx, consoleConfigName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	// Empty while the console operator is still rolling out, and permanently so
	// on a cluster installed without the console capability. Neither is an
	// error: the column simply stays unlinked.
	url, _, _ := unstructured.NestedString(obj.Object, "status", "consoleURL")
	if strings.TrimSpace(url) == "" {
		return nil, nil
	}

	return []model.Component{{
		Key:     model.KeyClusterConsole,
		Name:    "Web console",
		Group:   model.GroupOpenShift,
		Compare: model.CompareInfo,
		Kind:    "openshift",
		Version: strings.TrimSpace(url),
	}}, nil
}

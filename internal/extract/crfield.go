package extract

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/periscope/internal/model"
)

// crFieldExtractor reads a version from a nested field of every instance of a
// custom resource. The CSI driver versions Portworx and Dell manage live inside
// the CRs their operators reconcile, so we parse them out here.
//
// The field paths below are best-effort defaults, so verify them against your
// installs (`oc get <cr> -o yaml`) and adjust. Making these config-driven
// (paths in a values file) is a planned follow-up so a field rename doesn't
// require a rebuild.
type crFieldExtractor struct {
	key         string
	display     string
	kind        string
	gvr         schema.GroupVersionResource
	versionPath []string
	imageTag    bool // if set, take the substring after the last ':' (image ref tag)
}

// NewCRFieldExtractor builds a CR-field extractor from plain parameters, so
// extractors can be declared in config (see internal/config) without a rebuild.
func NewCRFieldExtractor(key, display, kind, group, version, resource string, versionPath []string, imageTag bool) Extractor {
	return crFieldExtractor{
		key:         key,
		display:     display,
		kind:        kind,
		gvr:         schema.GroupVersionResource{Group: group, Version: version, Resource: resource},
		versionPath: versionPath,
		imageTag:    imageTag,
	}
}

func (e crFieldExtractor) Key() string { return e.key }

func (e crFieldExtractor) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	list, err := c.Dynamic.Resource(e.gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil // this vendor's CRD isn't installed on this cluster
		}
		return nil, err
	}
	var out []model.Component
	for _, item := range list.Items {
		ver, _, _ := unstructured.NestedString(item.Object, e.versionPath...)
		if e.imageTag {
			ver = imageTagOf(ver)
		}
		out = append(out, model.Component{
			Key:       e.key,
			Name:      e.display,
			Group:     model.GroupOperators,
			Compare:   model.CompareVersion,
			Kind:      e.kind,
			Version:   ver,
			Namespace: item.GetNamespace(),
		})
	}
	return out, nil
}

// imageTagOf returns the tag of an image ref ("portworx/oci-monitor:3.1.0" -> "3.1.0").
func imageTagOf(image string) string {
	for i := len(image) - 1; i >= 0; i-- {
		switch image[i] {
		case ':':
			return image[i+1:]
		case '/':
			return image // no tag after the last path segment
		}
	}
	return image
}

// Portworx extracts the running Portworx version from the StorageCluster CR.
func Portworx() Extractor {
	return crFieldExtractor{
		key:     "portworx-csi",
		display: "Portworx (CSI)",
		kind:    "csi",
		gvr:     schema.GroupVersionResource{Group: "core.libopenstorage.org", Version: "v1", Resource: "storageclusters"},
		// status.version carries the running version; if your install leaves it
		// empty, switch to spec.image with imageTag:true.
		versionPath: []string{"status", "version"},
	}
}

// DellCSM extracts the managed CSI driver version from the ContainerStorageModule CR.
func DellCSM() Extractor {
	return crFieldExtractor{
		key:         "dell-csi",
		display:     "Dell CSM (CSI)",
		kind:        "csi",
		gvr:         schema.GroupVersionResource{Group: "storage.dell.com", Version: "v1", Resource: "containerstoragemodules"},
		versionPath: []string{"spec", "driver", "configVersion"},
	}
}

// Package config loads the optional extractor configuration, so CR field paths
// (Portworx, Dell, and future vendors) can be adjusted without recompiling.
package config

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/croz-ltd/periscope/internal/extract"
)

// CRExtractorSpec declares one custom-resource version extractor.
type CRExtractorSpec struct {
	Key         string   `json:"key"`
	Display     string   `json:"display"`
	Kind        string   `json:"kind"` // typically "csi" or "operator"
	Group       string   `json:"group"`
	Version     string   `json:"version"`
	Resource    string   `json:"resource"`
	VersionPath []string `json:"versionPath"`        // nested field path, e.g. ["spec","driver","configVersion"]
	ImageTag    bool     `json:"imageTag,omitempty"` // take the tag after the last ':' of the field value
}

// Config is the on-disk configuration file schema.
type Config struct {
	CRExtractors []CRExtractorSpec `json:"crExtractors"`
}

// BuildExtractors returns the extractor set. With no path it returns the
// built-in defaults. With a config file, the always-on extractors (OpenShift,
// OLM, nodes) are kept and the CR extractors come entirely from the file —
// letting operators redefine Portworx/Dell paths or add new vendors.
func BuildExtractors(path string) ([]extract.Extractor, error) {
	if path == "" {
		return extract.Default(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	exs := []extract.Extractor{extract.OpenShift{}, extract.OLM{}, extract.Nodes{}}
	for _, s := range cfg.CRExtractors {
		if s.Key == "" || s.Resource == "" {
			return nil, fmt.Errorf("crExtractor requires key and resource (got %+v)", s)
		}
		exs = append(exs, extract.NewCRFieldExtractor(
			s.Key, s.Display, s.Kind, s.Group, s.Version, s.Resource, s.VersionPath, s.ImageTag,
		))
	}
	return exs, nil
}

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/croz-ltd/periscope/internal/extract"
)

func keysOf(exs []extract.Extractor) map[string]bool {
	keys := make(map[string]bool, len(exs))
	for _, e := range exs {
		keys[e.Key()] = true
	}
	return keys
}

func TestBuildExtractorsDefaultsWithoutConfig(t *testing.T) {
	exs, err := BuildExtractors("")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(exs) != len(extract.Default()) {
		t.Errorf("got %d extractors, want the %d defaults", len(exs), len(extract.Default()))
	}
}

// A config file exists to fix a vendor CR field path. Losing the hand-written
// extractors as a side effect of that is how a fleet quietly stops reporting its
// certificates, and it used to happen because the kept set was a hard-coded list.
func TestBuildExtractorsKeepsHandWrittenExtractors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extractors.yaml")
	body := `crExtractors:
  - key: portworx-csi
    display: Portworx (CSI)
    kind: csi
    group: core.libopenstorage.org
    version: v1
    resource: storageclusters
    versionPath: [spec, image]
    imageTag: true
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	exs, err := BuildExtractors(path)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	keys := keysOf(exs)
	for _, want := range []string{"openshift", "olm", "nodes", "certificates", "grafana", "virtualization"} {
		if !keys[want] {
			t.Errorf("extractor %q was dropped by --config", want)
		}
	}
	if !keys["portworx-csi"] {
		t.Error("the configured CR extractor is missing")
	}
	// The file replaces the CR-field set, so the default it did not mention is gone.
	if keys["dell-csi"] {
		t.Error("dell-csi should be replaced by the config file, not kept")
	}
}

func TestBuildExtractorsRejectsIncompleteSpec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extractors.yaml")
	if err := os.WriteFile(path, []byte("crExtractors:\n  - display: No key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildExtractors(path); err == nil {
		t.Error("a CR extractor without key/resource should be rejected")
	}
}

package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// resource is the part of a manifest these tests compare.
type resource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Type       string `json:"type"`
	Metadata   struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	RoleRef struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"roleRef"`
	Subjects []struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"subjects"`
}

// parseDocs splits a multi-document YAML stream and drops the empty documents a
// leading comment block produces.
func parseDocs(t *testing.T, body string) []resource {
	t.Helper()
	var out []resource
	for _, doc := range strings.Split(body, "\n---") {
		if strings.TrimSpace(stripComments(doc)) == "" {
			continue
		}
		var r resource
		if err := yaml.Unmarshal([]byte(doc), &r); err != nil {
			t.Fatalf("document does not parse as YAML: %v\n%s", err, doc)
		}
		out = append(out, r)
	}
	return out
}

func stripComments(doc string) string {
	var kept []string
	for _, line := range strings.Split(doc, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func getJoinYAML(t *testing.T, query string) (*httptest.ResponseRecorder, []resource) {
	t.Helper()
	srv := &Server{}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/yaml/new-cluster"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	return rec, parseDocs(t, rec.Body.String())
}

// The whole point is `oc apply -f <url>`, so every document must parse and the
// four resources must be the ones that let a hub read this cluster.
func TestJoinYAMLServesApplyableResources(t *testing.T) {
	rec, docs := getJoinYAML(t, "")

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("Content-Type is %q, want application/yaml", ct)
	}
	if len(docs) != 4 {
		t.Fatalf("got %d resources, want 4: %+v", len(docs), docs)
	}

	kinds := map[string]resource{}
	for _, d := range docs {
		kinds[d.Kind] = d
	}
	for _, kind := range []string{"Namespace", "ServiceAccount", "ClusterRoleBinding", "Secret"} {
		if _, ok := kinds[kind]; !ok {
			t.Errorf("no %s in the document", kind)
		}
	}
	if got := kinds["ClusterRoleBinding"].RoleRef.Name; got != joinClusterRole {
		t.Errorf("bound to ClusterRole %q, want %q", got, joinClusterRole)
	}
	if subs := kinds["ClusterRoleBinding"].Subjects; len(subs) != 1 ||
		subs[0].Name != joinServiceAccount || subs[0].Namespace != joinNamespace {
		t.Errorf("binding subjects are %+v, want the reader SA in %s", subs, joinNamespace)
	}
	if got := kinds["Secret"].Type; got != "kubernetes.io/service-account-token" {
		t.Errorf("token Secret type is %q, want a service-account-token", got)
	}
	if got := kinds["Secret"].Metadata.Annotations["kubernetes.io/service-account.name"]; got != joinServiceAccount {
		t.Errorf("token Secret points at %q, want %q", got, joinServiceAccount)
	}
}

// A document that carries a credential must never be served over HTTP, and this
// one only asks the cluster to mint one. The comments talk about the token, so
// the check looks at the resources rather than the whole body.
func TestJoinYAMLCarriesNoCredential(t *testing.T) {
	rec, _ := getJoinYAML(t, "?name=prod-emea")
	// Line by line, because "metadata:" ends in the field this looks for.
	for _, line := range strings.Split(stripComments(rec.Body.String()), "\n") {
		field := strings.TrimSpace(line)
		for _, forbidden := range []string{"data:", "stringData:", "BEGIN CERTIFICATE", "BEGIN RSA"} {
			if strings.HasPrefix(field, forbidden) {
				t.Errorf("a resource in the document carries %q", line)
			}
		}
	}
}

// The name reaches the hub-side commands, and it labels the resources so the
// joined cluster records the name the hub knows it by.
func TestJoinYAMLNamesTheCluster(t *testing.T) {
	rec, docs := getJoinYAML(t, "?name=prod-emea")

	for _, d := range docs {
		if got := d.Metadata.Labels["periscope.io/cluster-name"]; got != "prod-emea" {
			t.Errorf("%s label is %q, want prod-emea", d.Kind, got)
		}
	}
	if !strings.Contains(rec.Body.String(), "create secret generic prod-emea") {
		t.Error("the instructions do not name the cluster")
	}
}

// Without a name the document still applies, and the instructions stay runnable
// by showing where the name goes.
func TestJoinYAMLWithoutAName(t *testing.T) {
	rec, docs := getJoinYAML(t, "")
	for _, d := range docs {
		if _, ok := d.Metadata.Labels["periscope.io/cluster-name"]; ok {
			t.Errorf("%s carries a cluster-name label with no name given", d.Kind)
		}
	}
	if !strings.Contains(rec.Body.String(), "<CLUSTER_NAME>") {
		t.Error("the instructions do not show where the name goes")
	}
}

// A name is interpolated into YAML, so anything that could break out of it has
// to be refused rather than escaped, and kubectl has to see the failure.
func TestJoinYAMLRejectsAnUnsafeName(t *testing.T) {
	for _, name := range []string{
		"prod emea",
		"Prod-EMEA",
		"prod\nkind: Pod",
		"../etc",
		strings.Repeat("a", clusterNameMax+1),
	} {
		srv := &Server{}
		rec := httptest.NewRecorder()
		target := "/yaml/new-cluster?" + url.Values{"name": {name}}.Encode()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("name %q returned %d, want 400", name, rec.Code)
		}
	}
}

// The endpoint and charts/periscope-join install the same access. A change to
// one alone means a cluster joined through the chart and a cluster joined
// through the URL do not carry the same RBAC.
func TestJoinYAMLMatchesChart(t *testing.T) {
	_, served := getJoinYAML(t, "")

	dir := filepath.Join("..", "..", "charts", "periscope-join", "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read chart templates: %v", err)
	}
	var chart []resource
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".yaml" {
			continue // NOTES.txt is prose, not a resource
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		// The chart is static, so its templates parse as plain YAML. A template
		// action appearing here is the signal to compare these by hand instead.
		if strings.Contains(string(body), "{{") {
			t.Skipf("%s is no longer static, compare it against join.go by hand", e.Name())
		}
		chart = append(chart, parseDocs(t, string(body))...)
	}

	identity := func(rs []resource) map[string]string {
		out := map[string]string{}
		for _, r := range rs {
			key := r.Kind
			out[key] = strings.Join([]string{
				r.APIVersion, r.Metadata.Name, r.Metadata.Namespace, r.Type,
				r.RoleRef.Name, r.Metadata.Annotations["kubernetes.io/service-account.name"],
			}, "|")
		}
		return out
	}
	got, want := identity(served), identity(chart)
	if len(got) != len(want) {
		t.Fatalf("endpoint serves %d kinds, chart installs %d", len(got), len(want))
	}
	for kind, w := range want {
		if got[kind] != w {
			t.Errorf("%s differs:\n  endpoint %s\n  chart    %s", kind, got[kind], w)
		}
	}
}

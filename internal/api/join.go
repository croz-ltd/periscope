package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"text/template"

	"github.com/croz-ltd/periscope/internal/logging"
)

// The join document served at GET /yaml/new-cluster, so a cluster is onboarded
// with one `oc apply -f <url>` and no Helm on the joined side.
//
// It renders the same four resources as charts/periscope-join. The chart stays
// the reference copy for a GitOps install, and this is the copy that travels
// over HTTP. Both are static, so they only drift if someone edits one alone:
// TestJoinYAMLMatchesChart compares them.
//
// Nothing here is a credential. The token is minted by the joined cluster after
// the apply, and the hub never sees it until an operator copies it over.

const (
	joinNamespace      = "periscope"
	joinServiceAccount = "periscope-reader"
	joinTokenSecret    = "periscope-reader-token"
	joinClusterRole    = "cluster-reader"
	clusterNameMax     = 63
)

// A cluster name reaches this document as a label value and as the name of the
// Secret an operator creates on the hub, so it must be a DNS-1123 label. The
// check also keeps a crafted name from breaking out of the rendered YAML.
var clusterNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// joinDoc is what the template renders from.
type joinDoc struct {
	Cluster        string // "" when the caller named no cluster
	Namespace      string // namespace created on the joined cluster
	HubNamespace   string // where the hub reads its cluster Secrets
	HubLabel       string // label that marks a cluster Secret on the hub
	ServiceAccount string
	TokenSecret    string
	ClusterRole    string
}

// ClusterOrPlaceholder keeps the instructions runnable when no name was given.
func (d joinDoc) ClusterOrPlaceholder() string {
	if d.Cluster == "" {
		return "<CLUSTER_NAME>"
	}
	return d.Cluster
}

var joinTemplate = template.Must(template.New("join").Parse(`# Periscope: join this cluster to the hub.
#
# Apply on the cluster you want to compare, NOT on the hub:
#
#     oc apply -f <this url>
#
# It creates a read-only ServiceAccount, binds it to {{ .ClusterRole }}, and asks
# for a long-lived token. It grants no write access and holds no credential.
#
# Then register the cluster on the HUB, in two steps.
#
# 1) On THIS cluster, read the API URL and the token:
#
#      API_URL=$(oc whoami --show-server)
#      TOKEN=$(oc -n {{ .Namespace }} get secret {{ .TokenSecret }} \
#        -o jsonpath='{.data.token}' | base64 -d)
#
#    Use the external API URL, not the in-cluster service, so the certificate
#    rows report the endpoint that clients really use.
#
# 2) On the HUB, create the labeled Secret. Its name is the name this cluster
#    gets in the matrix:
#
#      oc -n {{ .HubNamespace }} create secret generic {{ .ClusterOrPlaceholder }} \
#        --from-literal=apiURL="$API_URL" \
#        --from-literal=token="$TOKEN"
#
#      oc -n {{ .HubNamespace }} label secret {{ .ClusterOrPlaceholder }} {{ .HubLabel }}
#
#    Column order is optional. Lower sorts further left, and unlabeled clusters
#    sort to the right of labeled ones:
#
#      oc -n {{ .HubNamespace }} label secret {{ .ClusterOrPlaceholder }} periscope.io/order=10
#
# The hub picks the cluster up on its next scrape.
---
apiVersion: v1
kind: Namespace
metadata:
  name: {{ .Namespace }}
  labels:
    app.kubernetes.io/name: periscope-join
{{- with .Cluster }}
    periscope.io/cluster-name: {{ . }}
{{- end }}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .ServiceAccount }}
  namespace: {{ .Namespace }}
  labels:
    app.kubernetes.io/name: periscope-join
{{- with .Cluster }}
    periscope.io/cluster-name: {{ . }}
{{- end }}
---
# Read-only across the whole cluster via OpenShift's built-in {{ .ClusterRole }},
# so a new operator CRD never answers "forbidden" on the next scrape.
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ .ServiceAccount }}
  labels:
    app.kubernetes.io/name: periscope-join
{{- with .Cluster }}
    periscope.io/cluster-name: {{ . }}
{{- end }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ .ClusterRole }}
subjects:
  - kind: ServiceAccount
    name: {{ .ServiceAccount }}
    namespace: {{ .Namespace }}
---
# A hand-created ServiceAccount token Secret yields a long-lived token, which is
# what the hub stores to poll this cluster.
apiVersion: v1
kind: Secret
metadata:
  name: {{ .TokenSecret }}
  namespace: {{ .Namespace }}
  annotations:
    kubernetes.io/service-account.name: {{ .ServiceAccount }}
  labels:
    app.kubernetes.io/name: periscope-join
{{- with .Cluster }}
    periscope.io/cluster-name: {{ . }}
{{- end }}
type: kubernetes.io/service-account-token
`))

// handleJoinYAML renders the join manifests for one cluster.
//
// The response is a plain document with no server state in it, so a 400 on a bad
// name is the only failure. kubectl reports that status, which is why the name is
// rejected here rather than substituted with something safe.
func (s *Server) handleJoinYAML(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cluster := strings.TrimSpace(q.Get("name"))
	if cluster != "" && !validClusterName(cluster) {
		http.Error(w, fmt.Sprintf(
			"invalid name %q: use lower-case letters, digits and dashes, up to %d characters",
			cluster, clusterNameMax), http.StatusBadRequest)
		return
	}

	doc := joinDoc{
		Cluster:        cluster,
		Namespace:      joinNamespace,
		HubNamespace:   joinNamespace,
		HubLabel:       "periscope.io/cluster=true",
		ServiceAccount: joinServiceAccount,
		TokenSecret:    joinTokenSecret,
		ClusterRole:    joinClusterRole,
	}
	// The hub-side commands name the namespace and label this deployment actually
	// watches, so a hub running with non-default flags still prints commands that
	// work.
	if s.Scheduler != nil && s.Scheduler.Registry != nil {
		reg := s.Scheduler.Registry
		if reg.Namespace != "" {
			doc.HubNamespace = reg.Namespace
		}
		if reg.LabelKey != "" {
			doc.HubLabel = reg.LabelKey + "=" + reg.LabelVal
		}
	}

	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := joinTemplate.Execute(w, doc); err != nil {
		// The status is already sent, so the client sees a truncated document.
		// Saying so in the log is all that is left.
		logging.For("api").Error("cannot render the join document", "error", err)
	}
}

func validClusterName(name string) bool {
	return len(name) <= clusterNameMax && clusterNamePattern.MatchString(name)
}

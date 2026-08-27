package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/croz-ltd/periscope/internal/logging"
	"github.com/croz-ltd/periscope/internal/store"
)

// The admin API, mounted under /api/admin, is the part of Periscope that
// destroys data. Everything else either reads or adds.
//
// Removing a cluster from a fleet means deleting its credential Secret. The
// history stays behind, and the matrix keeps a column for a cluster nobody
// scrapes any more, greyed out as stale forever. Purging is how that column
// goes away.
//
// Two things gate it. The reader must hold an elevated right in the hub
// namespace (cluster.CanAdminister), which is checked on every request here and
// not merely at the door, so the endpoint is safe to expose. And a cluster is
// only purgeable once its join Secret is gone: the configuration decides what
// exists, and this API only clears up after it. That makes an accidental purge
// impossible to aim at a live cluster.

// adminMaxBody bounds a purge request. It carries a list of cluster names.
const adminMaxBody = 64 << 10

// purgeableCluster is one row of GET /api/admin/clusters: what the store holds
// for a cluster, and whether the fleet still includes it.
type purgeableCluster struct {
	store.StoredCluster
	// Joined is true while a labeled Secret for this cluster exists on the hub.
	// Only the others can be purged.
	Joined bool `json:"joined"`
}

// refusal explains one cluster the purge would not touch.
type refusal struct {
	Cluster string `json:"cluster"`
	Reason  string `json:"reason"`
}

type purgeResponse struct {
	Purged  []store.Purged `json:"purged"`
	Refused []refusal      `json:"refused,omitempty"`
}

// callerToken is the token the request is made with: the one oauth-proxy
// forwards for a signed-in reader (--pass-access-token), or one a command-line
// client presented itself. Empty when neither is there, which the access review
// treats as "ask with the hub's own rights", and in the cluster that is a no.
func callerToken(r *http.Request) string {
	if t := strings.TrimSpace(r.Header.Get("X-Forwarded-Access-Token")); t != "" {
		return t
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if rest, found := strings.CutPrefix(auth, "Bearer "); found {
		return strings.TrimSpace(rest)
	}
	return ""
}

// admitAdmin answers the request itself and returns false when the caller may
// not use the admin API. The UI reads the status code alone: 403 means do not
// offer purging to this reader, 503 means this hub cannot offer it at all.
//
// An operator needs more than the code. Three unrelated failures all end in 403
// (no token arrived, the token is not one the API server accepts, the review
// happened and said no) and they have three different fixes, so each says which
// it is in the body and in a log line at warn.
func (s *Server) admitAdmin(w http.ResponseWriter, r *http.Request) bool {
	log := logging.For("api")
	if s.Scheduler == nil || s.Scheduler.Registry == nil {
		http.Error(w, "this server has no cluster registry, so it cannot check administrative access",
			http.StatusServiceUnavailable)
		return false
	}
	reg := s.Scheduler.Registry
	token := callerToken(r)
	user := firstNonEmpty(
		r.Header.Get("X-Forwarded-Preferred-Username"),
		r.Header.Get("X-Forwarded-User"),
		r.Header.Get("X-Forwarded-Email"),
	)

	ok, err := reg.CanAdminister(r.Context(), token)
	if ok {
		log.Debug("admin request allowed", "path", r.URL.Path, "user", user)
		return true
	}

	switch {
	case token == "":
		// The common one, and the only one that is not about the caller at all:
		// the sidecar is not forwarding the token, so the review was answered
		// with this pod's own rights instead of the reader's.
		log.Warn("refused an admin request that carried no token",
			"path", r.URL.Path, "user", user, "signedIn", user != "")
		http.Error(w, "no token reached this server, so it cannot tell who is asking. "+
			"Its oauth-proxy sidecar needs --pass-access-token and --pass-user-bearer-token: "+
			"re-apply the periscope chart. Reaching this server directly, past the proxy, "+
			"lands here too.", http.StatusForbidden)
	case err != nil:
		log.Warn("refused an admin request: the access review could not be made",
			"path", r.URL.Path, "user", user, "error", err)
		http.Error(w, "the access review for this token could not be made, so the request "+
			"is refused: "+err.Error(), http.StatusForbidden)
	default:
		log.Warn("refused an admin request: the access review said no",
			"path", r.URL.Path, "user", user, "namespace", reg.Namespace)
		http.Error(w, "the admin API needs create on services in namespace "+reg.Namespace+
			", and the review for this token came back denied", http.StatusForbidden)
	}
	return false
}

// handleAdminClusters serves what the database holds per cluster, and whether
// the fleet still includes it. The UI calls this to decide two things at once:
// whether to offer purging at all (a 403 says no), and what there is to purge.
func (s *Server) handleAdminClusters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "use GET to list the stored clusters", http.StatusMethodNotAllowed)
		return
	}
	if !s.admitAdmin(w, r) {
		return
	}

	stored, joined, err := s.storedAndJoined(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]purgeableCluster, 0, len(stored))
	for _, c := range stored {
		out = append(out, purgeableCluster{StoredCluster: c, Joined: joined[c.Name]})
	}
	writeJSON(w, map[string]any{"clusters": out})
}

// handleAdminPurge removes the stored history of the named clusters. A cluster
// the fleet still includes is refused rather than purged, and said so by name:
// the honest answer to "purge this" for a live cluster is that its data is not
// stale, it is current.
func (s *Server) handleAdminPurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "use POST to purge cluster data", http.StatusMethodNotAllowed)
		return
	}
	if !s.admitAdmin(w, r) {
		return
	}

	var req struct {
		Clusters []string `json:"clusters"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, adminMaxBody)).Decode(&req); err != nil {
		http.Error(w, "cannot read the request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Clusters) == 0 {
		http.Error(w, "name at least one cluster to purge", http.StatusBadRequest)
		return
	}

	stored, joined, err := s.storedAndJoined(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	known := map[string]bool{}
	for _, c := range stored {
		known[c.Name] = true
	}

	log := logging.For("api")
	resp := purgeResponse{Purged: []store.Purged{}}
	// A name repeated in the request would otherwise be purged twice and
	// reported twice, the second time as zero rows.
	seen := map[string]bool{}
	for _, name := range req.Clusters {
		name = strings.TrimSpace(name)
		switch {
		case name == "" || seen[name]:
			continue
		case joined[name]:
			resp.Refused = append(resp.Refused, refusal{name,
				"still joined: delete its Secret on the hub first"})
		case !known[name]:
			resp.Refused = append(resp.Refused, refusal{name, "no stored data"})
		default:
			seen[name] = true
			purged, err := s.Store.PurgeCluster(name)
			if err != nil {
				log.Error("cannot purge the cluster", "cluster", name, "error", err)
				resp.Refused = append(resp.Refused, refusal{name, err.Error()})
				continue
			}
			log.Info("purged a cluster no longer joined", "cluster", name,
				"user", firstNonEmpty(r.Header.Get("X-Forwarded-Preferred-Username"), r.Header.Get("X-Forwarded-User")),
				"snapshots", purged.Snapshots, "changes", purged.Changes)
			resp.Purged = append(resp.Purged, purged)
		}
	}
	writeJSON(w, resp)
}

// storedAndJoined pairs what the database holds with what the fleet still
// includes. Both halves are read per request, because the answer changes the
// moment somebody deletes a Secret.
func (s *Server) storedAndJoined(r *http.Request) ([]store.StoredCluster, map[string]bool, error) {
	stored, err := s.Store.StoredClusters()
	if err != nil {
		return nil, nil, err
	}
	names, err := s.Scheduler.Registry.JoinedNames(r.Context())
	if err != nil {
		return nil, nil, err
	}
	joined := make(map[string]bool, len(names))
	for _, n := range names {
		joined[n] = true
	}
	return stored, joined, nil
}

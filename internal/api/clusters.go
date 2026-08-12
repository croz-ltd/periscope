package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/croz-ltd/periscope/internal/cluster"
	"github.com/croz-ltd/periscope/internal/logging"
	"github.com/croz-ltd/periscope/internal/provision"
)

// POST /api/clusters joins a cluster from credentials an operator pastes once.
//
// This is the one endpoint that writes. It writes on the target cluster with the
// operator's token, and in the hub namespace with the hub's own, so the app never
// holds a privilege the operator does not already have. The pasted token is used
// for the provisioning calls and dropped: what is stored is the read-only token
// the target cluster mints.
//
// It never appears in a log line or in a response. The request logger records the
// path and the query, and this endpoint takes its credentials in the body for
// exactly that reason.

// joinRequest is the body of POST /api/clusters.
type joinRequest struct {
	Name        string `json:"name"`
	APIURL      string `json:"apiURL"`
	Token       string `json:"token"`
	CABundle    string `json:"caBundle,omitempty"` // PEM
	InsecureTLS bool   `json:"insecureTLS,omitempty"`
	Order       *int   `json:"order,omitempty"`
}

type joinResponse struct {
	Name     string   `json:"name"`
	Created  bool     `json:"created"` // false means the credentials were replaced
	Actions  []string `json:"actions"`
	Warnings []string `json:"warnings,omitempty"`
}

// maxJoinBody bounds the request. A CA bundle is a few kilobytes, and nothing
// legitimate here is large.
const maxJoinBody = 256 << 10

func (s *Server) handleClusters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "use POST to join a cluster", http.StatusMethodNotAllowed)
		return
	}
	if s.Scheduler == nil || s.Scheduler.Registry == nil {
		http.Error(w, "this server has no cluster registry", http.StatusServiceUnavailable)
		return
	}
	reg := s.Scheduler.Registry
	ctx := r.Context()

	var req joinRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJoinBody)).Decode(&req); err != nil {
		http.Error(w, "cannot read the request: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.APIURL = strings.TrimSpace(req.APIURL)
	req.Token = strings.TrimSpace(req.Token)

	if !validClusterName(req.Name) {
		http.Error(w, fmt.Sprintf(
			"name must be lower-case letters, digits and dashes, up to %d characters", clusterNameMax),
			http.StatusBadRequest)
		return
	}
	if req.APIURL == "" || req.Token == "" {
		http.Error(w, "apiURL and token are both required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(req.APIURL, "https://") {
		// A token sent over plain HTTP is a token given away.
		http.Error(w, "apiURL must be an https:// URL", http.StatusBadRequest)
		return
	}
	if req.CABundle == "" && !req.InsecureTLS {
		http.Error(w, provision.ErrNoCABundle.Error(), http.StatusBadRequest)
		return
	}

	// Ask before trying. A hub whose Role was narrowed to read-only can still serve
	// the manifests, and saying which of the two applies is more use than a
	// forbidden error from halfway through the work.
	if !reg.CanJoinClusters(ctx) {
		http.Error(w, "this hub cannot write cluster Secrets in its namespace. "+
			"Grant create and update on secrets, or join the cluster with the manifests "+
			"from /yaml/new-cluster", http.StatusForbidden)
		return
	}

	log := logging.For("api")
	log.Info("joining a cluster", "cluster", req.Name, "host", req.APIURL,
		"tls", tlsDescription(req))

	creds := provision.Credentials{
		APIURL:      req.APIURL,
		Token:       req.Token,
		CABundle:    []byte(req.CABundle),
		InsecureTLS: req.InsecureTLS,
	}
	newClient := s.Provision
	if newClient == nil {
		newClient = provision.DefaultClientFactory
	}
	result, err := provision.Reader(ctx, creds, newClient)
	if err != nil {
		// The message names the object or the host that refused, which is what an
		// operator needs. It carries no credential: the token is never formatted in.
		log.Warn("cannot provision the cluster", "cluster", req.Name, "host", req.APIURL, "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	order := cluster.DefaultOrder
	if req.Order != nil {
		order = *req.Order
	}
	created, err := reg.SaveCluster(ctx, req.Name, req.APIURL, result.Token, []byte(req.CABundle), order)
	if err != nil {
		log.Error("cannot store the cluster Secret", "cluster", req.Name, "error", err)
		http.Error(w, "the cluster was prepared, but storing its Secret on the hub failed: "+
			err.Error(), http.StatusInternalServerError)
		return
	}

	actions := append(result.Actions, secretAction(created, req.Name, reg.Namespace))
	// The matrix is what the reader is waiting for, so do not make them find the
	// Refresh action to see the cluster they just joined. A scheduler with nowhere
	// to write cannot scrape, and claiming it started is a lie.
	if s.Scheduler.Store != nil {
		go s.Scheduler.ScrapeAll(context.Background())
		actions = append(actions, "started a scrape")
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, joinResponse{
		Name:     req.Name,
		Created:  created,
		Actions:  actions,
		Warnings: result.Warnings,
	})
}

func secretAction(created bool, name, namespace string) string {
	if created {
		return fmt.Sprintf("stored the credentials as %s/%s on the hub", namespace, name)
	}
	return fmt.Sprintf("replaced the credentials in %s/%s on the hub", namespace, name)
}

func tlsDescription(req joinRequest) string {
	if req.CABundle != "" {
		return "verified with the supplied CA bundle"
	}
	return "unverified"
}

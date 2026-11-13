package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/croz-ltd/periscope/internal/logging"
)

// Paths the guard serves itself.
const (
	PathLogin       = "/oauth/start"
	PathCallback    = "/oauth/callback"
	PathSignOut     = "/oauth/sign_out"
	PathMCPToken    = "/mcp/token"
	PathMCPCallback = "/mcp/token/callback"
)

// OAuthClient is one client registered with the OpenShift OAuth server.
type OAuthClient struct {
	ID string
	// Secret returns the client secret. It is read on every use, because the
	// ServiceAccount token that serves as the login client secret is rotated
	// by the kubelet.
	Secret func() (string, error)
}

// Endpoints are the parts of the OpenShift OAuth server metadata the login
// needs.
type Endpoints struct {
	Authorize string   `json:"authorization_endpoint"`
	Token     string   `json:"token_endpoint"`
	Methods   []string `json:"code_challenge_methods_supported"`
}

func (e *Endpoints) pkce() bool {
	for _, m := range e.Methods {
		if m == "S256" {
			return true
		}
	}
	return false
}

// Guard puts every request behind an OpenShift login and the read check.
type Guard struct {
	Reviewer Reviewer
	Sealer   *Sealer
	// Metadata returns the raw OAuth server metadata document
	// (/.well-known/oauth-authorization-server on the API server).
	Metadata func(ctx context.Context) ([]byte, error)
	// HTTP calls the token endpoint. Its trust store has to cover the
	// certificate of the oauth-openshift route.
	HTTP *http.Client
	// Login signs readers in to the UI. MCP issues long-lived tokens for MCP
	// clients and is nil when it is not configured.
	Login OAuthClient
	MCP   *OAuthClient
	// Namespace is the hub namespace, named in refusals.
	Namespace string
	// Public reports paths that need no login.
	Public func(path string) bool
	Now    func() time.Time

	mu        sync.Mutex
	endpoints *Endpoints
}

// scopes are the scopes Periscope asks for. They let the token tell who the
// user is and nothing else, see KubeReviewer.Allowed.
const scopes = "user:info user:check-access"

// ReadVerb and ReadResource are the right a reader needs: get on services in
// the hub namespace, the same check oauth-proxy made in 1.x.
const (
	ReadVerb     = "get"
	ReadResource = "services"
)

func (g *Guard) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// Wrap returns next behind the guard.
func (g *Guard) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case PathLogin:
			g.startFlow(w, r, "login", &g.Login, safeRedirect(r.URL.Query().Get("rd")))
			return
		case PathCallback:
			g.callback(w, r, nil)
			return
		case PathSignOut:
			clearCookie(w, r, sessionCookie)
			writePage(w, http.StatusOK, signedOutPage, nil)
			return
		}
		if g.Public != nil && g.Public(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		id, fromCookie, err := g.identify(r)
		if err != nil {
			g.unauthenticated(w, r, err)
			return
		}
		ok, err := g.Reviewer.Allowed(r.Context(), id, ReadVerb, ReadResource)
		if err != nil || !ok {
			g.forbidden(w, r, id, err)
			return
		}
		r = r.WithContext(WithIdentity(r.Context(), id))

		switch r.URL.Path {
		case PathMCPToken:
			if !fromCookie {
				http.Error(w, "open "+PathMCPToken+" in a browser, it signs you in to issue the token",
					http.StatusBadRequest)
				return
			}
			if g.MCP == nil {
				writePage(w, http.StatusOK, noMCPClientPage, tokenPageData(r, id, "", time.Time{}))
				return
			}
			g.startFlow(w, r, "mcp", g.MCP, "")
			return
		case PathMCPCallback:
			g.callback(w, r, id)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// identify finds the token of a request, from the Authorization header first
// and the session cookie second, and asks who it belongs to.
func (g *Guard) identify(r *http.Request) (*Identity, bool, error) {
	if token := bearer(r); token != "" {
		id, err := g.Reviewer.Authenticate(r.Context(), token)
		return id, false, err
	}
	sess, ok := g.Sealer.readSession(r, g.now())
	if !ok {
		return nil, false, errNoCredentials
	}
	id, err := g.Reviewer.Authenticate(r.Context(), sess.Token)
	return id, true, err
}

var errNoCredentials = errors.New("no token and no session")

func bearer(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

// isNavigation reports whether a person in a browser asked for a page, as
// opposed to a script or the UI fetching data. Only a navigation is sent to
// the login page. Everything else gets a 401 it can act on.
func isNavigation(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if r.Header.Get("Authorization") != "" {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func (g *Guard) unauthenticated(w http.ResponseWriter, r *http.Request, err error) {
	log := logging.For("auth")
	if isNavigation(r) {
		clearCookie(w, r, sessionCookie)
		rd := r.URL.RequestURI()
		http.Redirect(w, r, PathLogin+"?rd="+url.QueryEscape(rd), http.StatusFound)
		return
	}
	if !errors.Is(err, errNoCredentials) && !errors.Is(err, ErrInvalidToken) {
		log.Warn("cannot review a token", "path", r.URL.Path, "error", err)
	} else {
		log.Debug("unauthenticated request", "path", r.URL.Path, "reason", err)
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="periscope"`)
	msg := "sign in first: send an OpenShift token as Authorization: Bearer <token> " +
		"(oc whoami -t, or a token from " + PathMCPToken + ")"
	if errors.Is(err, ErrInvalidToken) {
		msg = "the token is expired, revoked or not valid on this cluster"
	}
	http.Error(w, msg, http.StatusUnauthorized)
}

func (g *Guard) forbidden(w http.ResponseWriter, r *http.Request, id *Identity, err error) {
	log := logging.For("auth")
	if err != nil {
		log.Warn("cannot review access", "user", id.User, "path", r.URL.Path, "error", err)
		http.Error(w, "the access review failed: "+err.Error(), http.StatusForbidden)
		return
	}
	log.Info("refused a reader without access", "user", id.User, "path", r.URL.Path)
	msg := fmt.Sprintf("%s cannot read Periscope. Reading needs %s on %s in namespace %s.",
		id.User, ReadVerb, ReadResource, g.Namespace)
	if isNavigation(r) {
		writePage(w, http.StatusForbidden, forbiddenPage, map[string]string{"Message": msg})
		return
	}
	http.Error(w, msg, http.StatusForbidden)
}

// safeRedirect keeps a post-login redirect on this host. Anything that is not
// a plain local path becomes "/".
func safeRedirect(rd string) string {
	if !strings.HasPrefix(rd, "/") || strings.HasPrefix(rd, "//") || strings.HasPrefix(rd, "/\\") {
		return "/"
	}
	return rd
}

// redirectURI is where the OAuth server sends the browser back to.
func redirectURI(r *http.Request, path string) string {
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + path
}

func (g *Guard) metadata(ctx context.Context) (*Endpoints, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.endpoints != nil {
		return g.endpoints, nil
	}
	raw, err := g.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot read the OAuth server metadata: %w", err)
	}
	var e Endpoints
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, fmt.Errorf("cannot parse the OAuth server metadata: %w", err)
	}
	if e.Authorize == "" || e.Token == "" {
		return nil, errors.New("the OAuth server metadata names no authorize or token endpoint")
	}
	g.endpoints = &e
	return &e, nil
}

// startFlow sends the browser to the OpenShift login page.
func (g *Guard) startFlow(w http.ResponseWriter, r *http.Request, kind string, client *OAuthClient, rd string) {
	ep, err := g.metadata(r.Context())
	if err != nil {
		logging.For("auth").Error("cannot start a login", "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	state, err1 := randomString(32)
	verifier, err2 := randomString(48)
	if err := errors.Join(err1, err2); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	f := flow{State: state, Verifier: verifier, Redirect: rd, Kind: kind, Expires: g.now().Add(flowTTL)}
	if err := g.Sealer.writeFlow(w, r, f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	path := PathCallback
	if kind == "mcp" {
		path = PathMCPCallback
	}
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {client.ID},
		"redirect_uri":  {redirectURI(r, path)},
		"scope":         {scopes},
		"state":         {state},
	}
	if ep.pkce() {
		sum := sha256.Sum256([]byte(verifier))
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
		q.Set("code_challenge_method", "S256")
	}
	http.Redirect(w, r, ep.Authorize+"?"+q.Encode(), http.StatusFound)
}

// callback finishes a login. For the UI login it writes the session cookie,
// for an MCP token it shows the token. signedIn is the reader of the browser
// session, set only for the MCP flow.
func (g *Guard) callback(w http.ResponseWriter, r *http.Request, signedIn *Identity) {
	log := logging.For("auth")
	q := r.URL.Query()
	f, ok := g.Sealer.readFlow(r, g.now())
	clearCookie(w, r, flowCookie)
	if !ok || subtle.ConstantTimeCompare([]byte(f.State), []byte(q.Get("state"))) != 1 {
		http.Error(w, "this login is expired or was not started here, start again", http.StatusBadRequest)
		return
	}
	if e := q.Get("error"); e != "" {
		http.Error(w, "the OpenShift login refused: "+e+" "+q.Get("error_description"), http.StatusForbidden)
		return
	}

	client, path := &g.Login, PathCallback
	if f.Kind == "mcp" {
		if signedIn == nil || g.MCP == nil {
			http.Error(w, "an MCP token needs a signed-in session", http.StatusBadRequest)
			return
		}
		client, path = g.MCP, PathMCPCallback
	} else if signedIn != nil {
		http.Error(w, "unexpected login state", http.StatusBadRequest)
		return
	}

	tok, err := g.exchange(r.Context(), client, q.Get("code"), redirectURI(r, path), f.Verifier)
	if err != nil {
		log.Error("cannot exchange the login code", "client", client.ID, "error", err)
		http.Error(w, "cannot finish the login: "+err.Error(), http.StatusBadGateway)
		return
	}
	id, err := g.Reviewer.Authenticate(r.Context(), tok.AccessToken)
	if err != nil {
		http.Error(w, "the new token does not pass review: "+err.Error(), http.StatusBadGateway)
		return
	}
	expires := g.now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	if tok.ExpiresIn <= 0 {
		expires = g.now().Add(maxSession)
	}

	if f.Kind == "mcp" {
		if id.User != signedIn.User {
			http.Error(w, "the token was issued to "+id.User+", not to "+signedIn.User, http.StatusForbidden)
			return
		}
		log.Info("issued an MCP token", "user", id.User, "expires", expires.UTC().Format(time.RFC3339))
		w.Header().Set("Cache-Control", "no-store")
		writePage(w, http.StatusOK, tokenPage, tokenPageData(r, id, tok.AccessToken, expires))
		return
	}

	ok, err = g.Reviewer.Allowed(r.Context(), id, ReadVerb, ReadResource)
	if err != nil || !ok {
		g.forbidden(w, r, id, err)
		return
	}
	sessExpires := expires
	if limit := g.now().Add(maxSession); sessExpires.After(limit) {
		sessExpires = limit
	}
	if err := g.Sealer.writeSession(w, r, session{Token: tok.AccessToken, User: id.User, Expires: sessExpires}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Info("signed in", "user", id.User)
	http.Redirect(w, r, safeRedirect(f.Redirect), http.StatusFound)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func (g *Guard) exchange(ctx context.Context, client *OAuthClient, code, redirect, verifier string) (*tokenResponse, error) {
	if code == "" {
		return nil, errors.New("the callback carries no code")
	}
	ep, err := g.metadata(ctx)
	if err != nil {
		return nil, err
	}
	secret, err := client.Secret()
	if err != nil {
		return nil, fmt.Errorf("cannot read the client secret: %w", err)
	}
	// The client goes in the body, not in Basic auth: a ServiceAccount client
	// ID holds colons, which Basic auth cannot carry unescaped.
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {client.ID},
		"client_secret": {secret},
	}
	if ep.pkce() {
		form.Set("code_verifier", verifier)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := g.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("token endpoint answered %d: %.200s", res.StatusCode, body)
	}
	if tok.Error != "" || tok.AccessToken == "" {
		return nil, fmt.Errorf("token endpoint answered %d: %s %s", res.StatusCode, tok.Error, tok.Description)
	}
	return &tok, nil
}

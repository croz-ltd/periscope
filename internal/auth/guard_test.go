package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	authnv1 "k8s.io/api/authentication/v1"
	authzv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// testReviewer knows a few tokens and who can read.
type testReviewer struct {
	users   map[string]string
	readers map[string]bool
}

func (r *testReviewer) Authenticate(_ context.Context, token string) (*Identity, error) {
	if u, ok := r.users[token]; ok {
		return &Identity{User: u, Token: token}, nil
	}
	return nil, ErrInvalidToken
}

func (r *testReviewer) Allowed(_ context.Context, id *Identity, verb, resource string) (bool, error) {
	return verb == ReadVerb && resource == ReadResource && r.readers[id.User], nil
}

// oauthServer is a fake OpenShift OAuth server. It checks the PKCE verifier
// and the client credentials, and issues the token named for each client.
type oauthServer struct {
	*httptest.Server
	challenges map[string]string // code -> challenge
	tokens     map[string]string // client -> token it issues
}

func newOAuthServer(t *testing.T) *oauthServer {
	o := &oauthServer{challenges: map[string]string{}, tokens: map[string]string{
		"system:serviceaccount:periscope:periscope": "sha256~login-token",
		"periscope-mcp": "sha256~mcp-token",
	}}
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		client := r.PostForm.Get("client_id")
		if r.PostForm.Get("client_secret") != "secret-of-"+client {
			http.Error(w, `{"error":"unauthorized_client"}`, http.StatusUnauthorized)
			return
		}
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != o.challenges[r.PostForm.Get("code")] {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": o.tokens[client], "expires_in": 3600})
	}))
	t.Cleanup(o.Close)
	return o
}

func testGuard(t *testing.T, o *oauthServer) *Guard {
	sealer, err := NewSealer([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	secret := func(id string) func() (string, error) {
		return func() (string, error) { return "secret-of-" + id, nil }
	}
	return &Guard{
		Reviewer: &testReviewer{
			users:   map[string]string{"sha256~login-token": "alice", "sha256~mcp-token": "alice", "bob-token": "bob"},
			readers: map[string]bool{"alice": true},
		},
		Sealer:    sealer,
		Namespace: "periscope",
		Metadata: func(context.Context) ([]byte, error) {
			return fmt.Appendf(nil, `{"authorization_endpoint":%q,"token_endpoint":%q,"code_challenge_methods_supported":["S256"]}`,
				o.URL+"/authorize", o.URL+"/token"), nil
		},
		HTTP:   o.Client(),
		Login:  OAuthClient{ID: "system:serviceaccount:periscope:periscope", Secret: secret("system:serviceaccount:periscope:periscope")},
		MCP:    &OAuthClient{ID: "periscope-mcp", Secret: secret("periscope-mcp")},
		Public: func(p string) bool { return p == "/healthz" },
	}
}

// whoami is the app behind the guard: it answers with the user it sees.
var whoami = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if id := FromContext(r.Context()); id != nil {
		_, _ = w.Write([]byte(id.User))
		return
	}
	_, _ = w.Write([]byte("anonymous"))
})

type browser struct {
	t       *testing.T
	h       http.Handler
	cookies map[string]*http.Cookie
}

func (b *browser) get(target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "https://periscope.apps.example"+target, nil)
	req.Header.Set("Accept", "text/html")
	for _, c := range b.cookies {
		if c.Path == "" || strings.HasPrefix(req.URL.Path, c.Path) {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return rec
}

// follow sends the browser to the authorize URL in a redirect, and back with a
// code, the way the OpenShift login does once the user is signed in.
func (b *browser) authorize(o *oauthServer, rec *httptest.ResponseRecorder, code string) string {
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || !strings.HasPrefix(loc.String(), o.URL+"/authorize") {
		b.t.Fatalf("redirect = %q, want the authorize endpoint", rec.Header().Get("Location"))
	}
	q := loc.Query()
	if q.Get("scope") != scopes || q.Get("code_challenge_method") != "S256" {
		b.t.Fatalf("authorize query = %v", q)
	}
	o.challenges[code] = q.Get("code_challenge")
	back, _ := url.Parse(q.Get("redirect_uri"))
	return back.Path + "?code=" + code + "&state=" + url.QueryEscape(q.Get("state"))
}

func TestLoginFlowEndsInASession(t *testing.T) {
	o := newOAuthServer(t)
	b := &browser{t: t, h: testGuard(t, o).Wrap(whoami), cookies: map[string]*http.Cookie{}}

	rec := b.get("/statistics?x=1")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/oauth/start?rd=%2Fstatistics%3Fx%3D1" {
		t.Fatalf("first visit = %d %q, want a redirect to the login", rec.Code, rec.Header().Get("Location"))
	}
	rec = b.get(rec.Header().Get("Location"))
	callback := b.authorize(o, rec, "code-1")
	if !strings.HasPrefix(callback, PathCallback) {
		t.Fatalf("callback = %q", callback)
	}

	rec = b.get(callback)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/statistics?x=1" {
		t.Fatalf("callback = %d %q (%s), want a redirect back", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if c := b.cookies[sessionCookie]; c == nil || !c.HttpOnly || !c.Secure {
		t.Fatalf("session cookie = %+v, want an HttpOnly, Secure cookie", c)
	}
	if strings.Contains(b.cookies[sessionCookie].Value, "login-token") {
		t.Error("the session cookie carries the token in clear")
	}

	if rec = b.get("/api/matrix"); rec.Body.String() != "alice" {
		t.Errorf("signed-in request saw %q, want alice", rec.Body)
	}

	// Signing out drops the session.
	b.get(PathSignOut)
	if rec = b.get("/"); rec.Code != http.StatusFound {
		t.Errorf("after sign out = %d, want a redirect to the login", rec.Code)
	}
}

func TestCallbackRefusesAForeignState(t *testing.T) {
	o := newOAuthServer(t)
	b := &browser{t: t, h: testGuard(t, o).Wrap(whoami), cookies: map[string]*http.Cookie{}}
	b.authorize(o, b.get(PathLogin), "code-2")
	if rec := b.get(PathCallback + "?code=code-2&state=forged"); rec.Code != http.StatusBadRequest {
		t.Errorf("forged state = %d, want 400", rec.Code)
	}
	if b.cookies[sessionCookie] != nil {
		t.Error("a forged state produced a session")
	}
}

func TestRequestsWithoutALogin(t *testing.T) {
	g := testGuard(t, newOAuthServer(t))
	h := g.Wrap(whoami)
	serve := func(method, path string, header map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		for k, v := range header {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(http.MethodGet, "/api/matrix", map[string]string{"Accept": "application/json"}); rec.Code != http.StatusUnauthorized ||
		!strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Errorf("API without a login = %d, want 401 with WWW-Authenticate", rec.Code)
	}
	if rec := serve(http.MethodPost, "/mcp", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("MCP without a token = %d, want 401", rec.Code)
	}
	if rec := serve(http.MethodGet, "/healthz", nil); rec.Body.String() != "anonymous" {
		t.Errorf("public path = %q, want it served without a login", rec.Body)
	}
	if rec := serve(http.MethodGet, "/api/matrix", map[string]string{"Authorization": "Bearer sha256~mcp-token"}); rec.Body.String() != "alice" {
		t.Errorf("bearer token = %q, want alice", rec.Body)
	}
	rec := serve(http.MethodGet, "/api/matrix", map[string]string{"Authorization": "Bearer bob-token"})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "get on services in namespace periscope") {
		t.Errorf("user without access = %d %q, want 403 that names the right", rec.Code, rec.Body)
	}
	if rec := serve(http.MethodGet, "/", map[string]string{"Authorization": "Bearer rubbish", "Accept": "text/html"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad bearer token from a browser = %d, want 401, not a login loop", rec.Code)
	}
}

func TestMCPTokenFlow(t *testing.T) {
	o := newOAuthServer(t)
	g := testGuard(t, o)
	b := &browser{t: t, h: g.Wrap(whoami), cookies: map[string]*http.Cookie{}}
	b.get(b.authorize(o, b.get(PathLogin), "login"))

	rec := b.get(PathMCPToken)
	callback := b.authorize(o, rec, "mcp")
	if !strings.Contains(rec.Header().Get("Location"), "client_id=periscope-mcp") || !strings.HasPrefix(callback, PathMCPCallback) {
		t.Fatalf("MCP flow went to %q", rec.Header().Get("Location"))
	}
	rec = b.get(callback)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != mcpDocsPage {
		t.Fatalf("MCP callback = %d %q, want a redirect to the MCP page", rec.Code, rec.Header().Get("Location"))
	}
	if strings.Contains(rec.Header().Get("Location"), "mcp-token") {
		t.Error("the token is in the redirect URL")
	}
	c := b.cookies[issuedCookie]
	if c == nil || c.Path != PathMCPIssued || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("hand-over cookie = %+v, want HttpOnly, strict, scoped to %s", c, PathMCPIssued)
	}

	// The UI fetches the token once. The cookie path matters in a browser,
	// so the test browser sends it only to that path, like a real one.
	rec = b.get(PathMCPIssued)
	var got IssuedToken
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("issued = %d %s", rec.Code, rec.Body)
	}
	if got.Token != "sha256~mcp-token" || got.User != "alice" || got.Name != tokenObjectName("sha256~mcp-token") ||
		rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("issued = %+v", got)
	}
	if rec = b.get(PathMCPIssued); rec.Code != http.StatusNoContent {
		t.Errorf("second fetch = %d, want 204: the token is handed over once", rec.Code)
	}

	// Without an MCP client the token link leads to the MCP page, which
	// explains the fallback.
	g.MCP = nil
	if rec := b.get(PathMCPToken); rec.Code != http.StatusFound || rec.Header().Get("Location") != mcpDocsPage {
		t.Errorf("token link without an MCP client = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// A hand-over cookie of one user is not handed to another.
func TestIssuedTokenStaysWithItsUser(t *testing.T) {
	g := testGuard(t, newOAuthServer(t))
	g.Reviewer.(*testReviewer).readers["bob"] = true
	h := g.Wrap(whoami)

	rec := httptest.NewRecorder()
	if err := g.Sealer.writeIssued(rec, httptest.NewRequest(http.MethodGet, "/", nil), issuedToken{
		Token: "sha256~mcp-token", User: "alice", Expires: time.Now().Add(time.Hour), Until: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, PathMCPIssued, nil)
	req.Header.Set("Authorization", "Bearer bob-token")
	req.AddCookie(rec.Result().Cookies()[0])
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusNoContent || strings.Contains(out.Body.String(), "mcp-token") {
		t.Errorf("bob got %d %q, want nothing", out.Code, out.Body)
	}
}

func TestSafeRedirect(t *testing.T) {
	for in, want := range map[string]string{
		"/x?y=1": "/x?y=1", "": "/", "https://evil.example": "/", "//evil.example": "/", "/\\evil.example": "/",
	} {
		if got := safeRedirect(in); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}

// The reviewer asks the API server once per token and per right, and asks
// again when the answer is older than the cache allows.
func TestKubeReviewerCaches(t *testing.T) {
	cs := fake.NewClientset()
	tokenReviews, accessReviews := 0, 0
	cs.PrependReactor("create", "tokenreviews", func(a k8stesting.Action) (bool, runtime.Object, error) {
		tokenReviews++
		tr := a.(k8stesting.CreateAction).GetObject().(*authnv1.TokenReview)
		tr.Status = authnv1.TokenReviewStatus{Authenticated: tr.Spec.Token == "good",
			User: authnv1.UserInfo{Username: "alice", Groups: []string{"admins"}}}
		return true, tr, nil
	})
	cs.PrependReactor("create", "subjectaccessreviews", func(a k8stesting.Action) (bool, runtime.Object, error) {
		accessReviews++
		sar := a.(k8stesting.CreateAction).GetObject().(*authzv1.SubjectAccessReview)
		ok := sar.Spec.User == "alice" && sar.Spec.Groups[0] == "admins" && sar.Spec.ResourceAttributes.Namespace == "periscope"
		sar.Status.Allowed = ok
		return true, sar, nil
	})
	now := time.Unix(0, 0)
	k := &KubeReviewer{Client: cs, Namespace: "periscope", Now: func() time.Time { return now }}
	ctx := context.Background()

	for range 3 {
		id, err := k.Authenticate(ctx, "good")
		if err != nil || id.User != "alice" || id.Token != "good" {
			t.Fatalf("Authenticate = %+v, %v", id, err)
		}
		if ok, err := k.Allowed(ctx, id, "get", "services"); !ok || err != nil {
			t.Fatalf("Allowed = %v, %v", ok, err)
		}
	}
	if tokenReviews != 1 || accessReviews != 1 {
		t.Errorf("reviews = %d token, %d access, want one each", tokenReviews, accessReviews)
	}
	if _, err := k.Authenticate(ctx, "bad"); err != ErrInvalidToken {
		t.Errorf("bad token = %v, want ErrInvalidToken", err)
	}

	now = now.Add(2 * cacheTTL)
	id, _ := k.Authenticate(ctx, "good")
	_, _ = k.Allowed(ctx, id, "get", "services")
	if tokenReviews != 3 || accessReviews != 2 {
		t.Errorf("after expiry: %d token, %d access reviews, want the cache refreshed", tokenReviews, accessReviews)
	}
}

func TestSealerRejectsTampering(t *testing.T) {
	s, _ := NewSealer([]byte("0123456789abcdef0123456789abcdef"))
	v, err := s.seal(sessionCookie, session{Token: "t", User: "u", Expires: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	var out session
	if err := s.open(sessionCookie, v, &out); err != nil || out.User != "u" {
		t.Fatalf("open = %+v, %v", out, err)
	}
	if s.open(flowCookie, v, &out) == nil {
		t.Error("a session value opened as a flow cookie")
	}
	tampered := []byte(v)
	tampered[len(tampered)-2] ^= 1
	if s.open(sessionCookie, string(tampered), &out) == nil {
		t.Error("a tampered cookie opened")
	}
	if _, err := NewSealer([]byte("short")); err == nil {
		t.Error("a short secret was accepted")
	}
}

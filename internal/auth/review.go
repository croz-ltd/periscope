// Package auth signs readers in and checks what they are allowed to do. It replaces the
// oauth-proxy sidecar that guarded Periscope up to 1.x.
//
// Every request carries a token, either in an Authorization header (a script, an
// MCP client, Prometheus) or inside the session cookie a browser got at login.
// The hub asks the API server who the token belongs to (a TokenReview), then
// whether that user can read the dashboard (a SubjectAccessReview). Both answers
// are cached for a short time, so a busy page does not cost two API calls per
// request, and a revoked token or a removed role binding still stops working
// within a minute.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	authnv1 "k8s.io/api/authentication/v1"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Identity is the user behind a request.
type Identity struct {
	User   string
	UID    string
	Groups []string
	Extra  map[string][]string
	// Token is the credential the request was made with. It is kept so a check
	// made later in the request (the admin API) asks about the same caller.
	Token string
}

type identityKey struct{}

// WithIdentity returns a context that carries id.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// FromContext returns the identity of the request, or nil when the hub runs
// without authentication.
func FromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityKey{}).(*Identity)
	return id
}

// ErrInvalidToken means the API server does not accept the token: it is
// expired, revoked, or not a token at all.
var ErrInvalidToken = errors.New("the API server does not accept this token")

// Reviewer answers the two questions every request asks.
type Reviewer interface {
	// Authenticate returns the user a token belongs to.
	Authenticate(ctx context.Context, token string) (*Identity, error)
	// Allowed reports whether the user is allowed verb on resource in the hub
	// namespace.
	Allowed(ctx context.Context, id *Identity, verb, resource string) (bool, error)
}

// cacheTTL is how long an answer is reused. It is the longest a revoked token
// or a removed role binding keeps working.
const cacheTTL = time.Minute

// maxCacheEntries bounds the caches. Expired entries are dropped when the
// bound is reached, so a flood of distinct tokens cannot grow memory.
const maxCacheEntries = 4096

// KubeReviewer asks the hub's API server. Its client needs create on
// tokenreviews and subjectaccessreviews, which is what system:auth-delegator
// grants.
type KubeReviewer struct {
	Client    kubernetes.Interface
	Namespace string
	Now       func() time.Time // for tests, nil means time.Now

	mu      sync.Mutex
	authn   map[string]cachedIdentity
	allowed map[string]cachedAnswer
}

type cachedIdentity struct {
	id    *Identity
	until time.Time
}

type cachedAnswer struct {
	ok    bool
	until time.Time
}

func (k *KubeReviewer) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

// tokenKey is the cache key of a token. The cache holds a hash, never the
// token itself.
func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (k *KubeReviewer) Authenticate(ctx context.Context, token string) (*Identity, error) {
	if token == "" {
		return nil, ErrInvalidToken
	}
	key := tokenKey(token)
	now := k.now()
	k.mu.Lock()
	if c, ok := k.authn[key]; ok && now.Before(c.until) {
		k.mu.Unlock()
		id := *c.id
		id.Token = token
		return &id, nil
	}
	k.mu.Unlock()

	review := &authnv1.TokenReview{Spec: authnv1.TokenReviewSpec{Token: token}}
	res, err := k.Client.AuthenticationV1().TokenReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	if !res.Status.Authenticated {
		return nil, ErrInvalidToken
	}
	u := res.Status.User
	id := &Identity{User: u.Username, UID: u.UID, Groups: u.Groups, Extra: map[string][]string{}}
	for key, values := range u.Extra {
		id.Extra[key] = values
	}

	k.mu.Lock()
	if k.authn == nil {
		k.authn = map[string]cachedIdentity{}
	}
	pruneIdentities(k.authn, now)
	k.authn[key] = cachedIdentity{id: id, until: now.Add(cacheTTL)}
	k.mu.Unlock()

	out := *id
	out.Token = token
	return &out, nil
}

// Allowed asks with the user and the groups of the token, and leaves out the
// token scopes. An OpenShift token issued to Periscope has the scopes
// user:info and user:check-access only, which make it useless against the API
// server. The question here is whether the person can read the dashboard, so
// it is asked for the person, not for the scoped token.
func (k *KubeReviewer) Allowed(ctx context.Context, id *Identity, verb, resource string) (bool, error) {
	if id == nil {
		return false, nil
	}
	key := id.User + "\x00" + verb + "\x00" + resource
	for _, g := range id.Groups {
		key += "\x00" + g
	}
	now := k.now()
	k.mu.Lock()
	if c, ok := k.allowed[key]; ok && now.Before(c.until) {
		k.mu.Unlock()
		return c.ok, nil
	}
	k.mu.Unlock()

	review := &authzv1.SubjectAccessReview{Spec: authzv1.SubjectAccessReviewSpec{
		User:   id.User,
		UID:    id.UID,
		Groups: id.Groups,
		ResourceAttributes: &authzv1.ResourceAttributes{
			Namespace: k.Namespace, Verb: verb, Resource: resource, Version: "v1",
		},
	}}
	res, err := k.Client.AuthorizationV1().SubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}

	k.mu.Lock()
	if k.allowed == nil {
		k.allowed = map[string]cachedAnswer{}
	}
	pruneAnswers(k.allowed, now)
	k.allowed[key] = cachedAnswer{ok: res.Status.Allowed, until: now.Add(cacheTTL)}
	k.mu.Unlock()
	return res.Status.Allowed, nil
}

func pruneIdentities(m map[string]cachedIdentity, now time.Time) {
	if len(m) < maxCacheEntries {
		return
	}
	for k, v := range m {
		if !now.Before(v.until) {
			delete(m, k)
		}
	}
	if len(m) >= maxCacheEntries {
		clear(m) // all live: start over rather than grow
	}
}

func pruneAnswers(m map[string]cachedAnswer, now time.Time) {
	if len(m) < maxCacheEntries {
		return
	}
	for k, v := range m {
		if !now.Before(v.until) {
			delete(m, k)
		}
	}
	if len(m) >= maxCacheEntries {
		clear(m)
	}
}

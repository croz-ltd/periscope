package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Cookies keep their state on the client, encrypted and authenticated with a
// key only the hub knows. The hub then needs no session store, and a restart
// does not sign anybody out.

const (
	sessionCookie = "periscope_session"
	flowCookie    = "periscope_oauth"
	// flowTTL bounds one login: the time from the redirect to the OpenShift
	// login page until the callback.
	flowTTL = 10 * time.Minute
	// maxSession bounds a browser session even when the token lives longer.
	maxSession = 24 * time.Hour
)

// session is what the session cookie holds.
type session struct {
	Token   string    `json:"t"`
	User    string    `json:"u"`
	Expires time.Time `json:"e"`
}

// flow is the state of one login in progress.
type flow struct {
	State    string    `json:"s"`
	Verifier string    `json:"v"`
	Redirect string    `json:"r"`
	Kind     string    `json:"k"` // login | mcp
	Expires  time.Time `json:"e"`
}

// Sealer encrypts and decrypts cookie values with AES-256-GCM.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer derives the key from a secret of any length.
func NewSealer(secret []byte) (*Sealer, error) {
	if len(secret) < 16 {
		return nil, errors.New("the session secret is shorter than 16 bytes")
	}
	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// seal encrypts v. The cookie name is bound into the ciphertext, so the value
// of one cookie cannot be replayed as another.
func (s *Sealer) seal(name string, v any) (string, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := s.aead.Seal(nonce, nonce, plain, []byte(name))
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func (s *Sealer) open(name, value string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	n := s.aead.NonceSize()
	if len(raw) < n {
		return errors.New("cookie too short")
	}
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], []byte(name))
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, v)
}

func (s *Sealer) readSession(r *http.Request, now time.Time) (*session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, false
	}
	var sess session
	if s.open(sessionCookie, c.Value, &sess) != nil || !now.Before(sess.Expires) || sess.Token == "" {
		return nil, false
	}
	return &sess, true
}

func (s *Sealer) writeSession(w http.ResponseWriter, r *http.Request, sess session) error {
	value, err := s.seal(sessionCookie, sess)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: value, Path: "/",
		Expires: sess.Expires, HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Sealer) readFlow(r *http.Request, now time.Time) (*flow, bool) {
	c, err := r.Cookie(flowCookie)
	if err != nil {
		return nil, false
	}
	var f flow
	if s.open(flowCookie, c.Value, &f) != nil || !now.Before(f.Expires) {
		return nil, false
	}
	return &f, true
}

func (s *Sealer) writeFlow(w http.ResponseWriter, r *http.Request, f flow) error {
	value, err := s.seal(flowCookie, f)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: flowCookie, Value: value, Path: "/",
		Expires: f.Expires, HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

// isHTTPS reports whether the browser reached the hub over HTTPS. Behind the
// OpenShift router the hub serves TLS itself (reencrypt), so r.TLS is set. The
// forwarded header covers an edge-terminated route.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// randomString returns n random bytes, URL-safe encoded.
func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

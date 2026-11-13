package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	"net/http"
	"strings"
)

// The two pages the guard serves itself: signed out, and no access. They show
// instead of the UI, which a reader without a session or without access cannot
// load, so they are plain HTML in the colours of the UI.

const pageHead = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Periscope</title>
<style>
body{font-family:"Red Hat Text",system-ui,sans-serif;margin:0;background:#f2f2f2;color:#151515}
header{background:#fff;border-bottom:1px solid #d2d2d2;padding:.9rem 1.5rem;display:flex;align-items:center;gap:.75rem}
.logo{width:2rem;height:2rem;border-radius:6px;background:#0066cc}
.brand small{display:block;font-size:.7rem;letter-spacing:.08em;color:#4d4d4d;font-weight:600}
.brand strong{font-family:"Red Hat Display",system-ui,sans-serif;font-size:1.35rem;font-weight:500}
main{max-width:46rem;margin:2.5rem auto;background:#fff;padding:2rem 2.5rem;border-radius:16px}
h1{font-family:"Red Hat Display",system-ui,sans-serif;font-size:1.6rem;font-weight:500;margin-top:0}
.muted{color:#4d4d4d}
a.button{display:inline-block;background:#0066cc;color:#fff;padding:.45rem 1.4rem;border-radius:999px;text-decoration:none}
a{color:#0066cc}
</style></head><body>
<header><span class="logo"></span><span class="brand"><small>OPENSHIFT FLEET</small><strong>Periscope</strong></span></header>
<main>`

const pageFoot = `</main></body></html>`

var (
	signedOutPage = template.Must(template.New("out").Parse(pageHead + `
<h1>You are signed out</h1>
<p>Your Periscope session is closed. Your OpenShift login stays open, so signing in
again does not ask for a password.</p>
<p><a class="button" href="/">Sign in again</a></p>` + pageFoot))

	forbiddenPage = template.Must(template.New("forbidden").Parse(pageHead + `
<h1>No access</h1>
<p>{{.Message}}</p>
<p class="muted">Ask an administrator of the hub namespace for the role, then
<a href="/">try again</a>. To use another account, <a href="/oauth/sign_out">sign out</a>.</p>` + pageFoot))
)

// tokenObjectName is the name of the UserOAuthAccessToken that stores a token
// of the form sha256~<secret>. OpenShift names the object after a hash of the
// secret, so the token itself is never stored.
func tokenObjectName(token string) string {
	secret, ok := strings.CutPrefix(token, "sha256~")
	if !ok {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return "sha256~" + base64.RawURLEncoding.EncodeToString(sum[:])
}

func writePage(w http.ResponseWriter, status int, t *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = t.Execute(w, data)
}

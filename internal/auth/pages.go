package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	"net/http"
	"strings"
	"time"
)

// The few pages the guard serves itself. They are plain HTML, because they show
// before the UI loads (or instead of it), and the token page has to work with
// no script at all.

const pageHead = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Periscope</title>
<style>
body{font-family:"Red Hat Text",system-ui,sans-serif;margin:0;background:#f2f2f2;color:#151515}
main{max-width:52rem;margin:3rem auto;background:#fff;padding:2rem 2.5rem;border-radius:8px}
h1{font-family:"Red Hat Display",system-ui,sans-serif;font-size:1.6rem;margin-top:0}
h2{font-size:1.1rem;margin-top:2rem}
pre,textarea{font-family:"Red Hat Mono",ui-monospace,monospace;font-size:.85rem;background:#f5f5f5;
border:1px solid #d2d2d2;border-radius:4px;padding:.75rem;width:100%;box-sizing:border-box;white-space:pre-wrap;word-break:break-all}
textarea{height:4.5rem;resize:none}
.muted{color:#6a6e73}
a{color:#0066cc}
</style></head><body><main>`

const pageFoot = `</main></body></html>`

var (
	signedOutPage = template.Must(template.New("out").Parse(pageHead + `
<h1>You are signed out</h1>
<p>Your Periscope session is closed. Your OpenShift login stays open, so signing in
again does not ask for a password.</p>
<p><a href="/">Sign in again</a></p>` + pageFoot))

	forbiddenPage = template.Must(template.New("forbidden").Parse(pageHead + `
<h1>No access</h1>
<p>{{.Message}}</p>
<p class="muted">Ask an administrator of the hub namespace for the role, then
<a href="/">try again</a>. To use another account, <a href="/oauth/sign_out">sign out</a>.</p>` + pageFoot))

	noMCPClientPage = template.Must(template.New("nomcp").Parse(pageHead + `
<h1>MCP access</h1>
<p>This hub has no OAuth client for MCP tokens, so it cannot issue one. Set
<code>host</code> in the periscope chart values, or let the chart find the cluster
domain, to create it.</p>
<p>Until then, an MCP client can use your own OpenShift token. It expires with your
login, often after 24 hours.</p>
<pre>claude mcp add --transport http periscope {{.URL}} \
  --header "Authorization: Bearer $(oc whoami -t)"</pre>` + pageFoot))

	tokenPage = template.Must(template.New("token").Parse(pageHead + `
<h1>Your MCP token</h1>
<p>Issued to <strong>{{.User}}</strong>. It is valid until <strong>{{.Expires}}</strong>.
It reads Periscope with your rights, and nothing else on the cluster.</p>
<p class="muted">Copy it now. Periscope does not keep it, and this page does not show it again.</p>
<textarea readonly onclick="this.select()">{{.Token}}</textarea>

<h2>Claude Code</h2>
<pre>claude mcp add --transport http periscope {{.URL}} \
  --header "Authorization: Bearer {{.Token}}"</pre>

<h2>Other MCP clients</h2>
<pre>{
  "mcpServers": {
    "periscope": {
      "type": "http",
      "url": "{{.URL}}",
      "headers": { "Authorization": "Bearer {{.Token}}" }
    }
  }
}</pre>
{{if .Name}}
<h2>Revoke it</h2>
<p>The token is an OpenShift OAuth token of your user. To end it before it expires:</p>
<pre>oc delete useroauthaccesstoken {{.Name}}</pre>
{{end}}
<p><a href="/">Back to Periscope</a></p>` + pageFoot))
)

type tokenData struct {
	User, Token, Name, URL, Expires string
}

func tokenPageData(r *http.Request, id *Identity, token string, expires time.Time) tokenData {
	return tokenData{
		User:    id.User,
		Token:   token,
		Name:    tokenObjectName(token),
		URL:     redirectURI(r, "/mcp"),
		Expires: expires.UTC().Format("2006-01-02 15:04 UTC"),
	}
}

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

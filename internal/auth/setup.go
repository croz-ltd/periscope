package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// In-pod paths of the ServiceAccount files.
const (
	saTokenFile     = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	saCAFile        = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	serviceCAFile   = "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"
	metadataPath    = "/.well-known/oauth-authorization-server"
	tokenHTTPTimout = 15 * time.Second
)

// Config is what the hub needs to sign readers in.
type Config struct {
	Namespace         string
	SessionSecretFile string
	// LoginClientID is the OAuth client of the UI login. The default is the
	// hub ServiceAccount, which OpenShift accepts as an OAuth client when it
	// carries an oauth-redirectreference annotation.
	LoginClientID   string
	LoginSecretFile string // empty means the ServiceAccount token
	// MCPClientID names the OAuthClient that issues MCP tokens. Empty turns
	// the token page into instructions for oc whoami -t.
	MCPClientID         string
	MCPClientSecretFile string
	// CAFiles add to the trust store used for the OAuth token endpoint.
	CAFiles []string
	Public  func(path string) bool
}

// New builds the guard against the hub cluster.
func New(cfg Config, hub *rest.Config) (*Guard, error) {
	client, err := kubernetes.NewForConfig(hub)
	if err != nil {
		return nil, err
	}
	secret, err := os.ReadFile(cfg.SessionSecretFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read the session secret: %w", err)
	}
	sealer, err := NewSealer([]byte(strings.TrimSpace(string(secret))))
	if err != nil {
		return nil, err
	}
	if cfg.LoginClientID == "" {
		return nil, errors.New("no OAuth client for the login")
	}
	loginSecret := cfg.LoginSecretFile
	if loginSecret == "" {
		loginSecret = saTokenFile
	}

	pool, err := trustStore(cfg.CAFiles)
	if err != nil {
		return nil, err
	}
	g := &Guard{
		Reviewer:  &KubeReviewer{Client: client, Namespace: cfg.Namespace},
		Sealer:    sealer,
		Namespace: cfg.Namespace,
		Public:    cfg.Public,
		Metadata: func(ctx context.Context) ([]byte, error) {
			return client.Discovery().RESTClient().Get().AbsPath(metadataPath).DoRaw(ctx)
		},
		HTTP: &http.Client{
			Timeout:   tokenHTTPTimout,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		},
		Login: OAuthClient{ID: cfg.LoginClientID, Secret: fileSecret(loginSecret)},
	}
	if cfg.MCPClientID != "" {
		g.MCP = &OAuthClient{ID: cfg.MCPClientID, Secret: fileSecret(cfg.MCPClientSecretFile)}
	}
	return g, nil
}

func fileSecret(path string) func() (string, error) {
	return func() (string, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
}

// trustStore is the system store plus the cluster CAs. On OpenShift the
// ServiceAccount CA bundle also holds the CA of the default ingress
// certificate, which is what the oauth-openshift route serves.
func trustStore(extra []string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	for _, path := range append([]string{saCAFile, serviceCAFile}, extra...) {
		pem, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) && (path == saCAFile || path == serviceCAFile) {
				continue // not in a pod
			}
			return nil, fmt.Errorf("cannot read CA file %s: %w", path, err)
		}
		pool.AppendCertsFromPEM(pem)
	}
	return pool, nil
}

package extract

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/periscope/internal/model"
)

// Certificates reports the expiry of the API server and default ingress
// (router wildcard) serving certificates. These are judged against absolute
// thresholds (see model.CompareExpiry), not compared between clusters.
type Certificates struct{}

func (Certificates) Key() string { return "certificates" }

var ingressConfigGVR = schema.GroupVersionResource{
	Group: "config.openshift.io", Version: "v1", Resource: "ingresses",
}

func (Certificates) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	var out []model.Component

	// API server serving cert: dial the API host directly.
	out = append(out, expiryComponent(ctx, "cert-api", "API", apiHostPort(c.Host), hostOnly(c.Host)))

	// Default ingress wildcard: resolve the apps domain, then dial an arbitrary
	// host under it (the router serves the default cert for unknown hosts).
	if domain := ingressDomain(ctx, c); domain != "" {
		host := "periscope-certcheck." + domain
		out = append(out, expiryComponent(ctx, "cert-ingress", "Ingress", host+":443", host))
	} else {
		out = append(out, model.Component{
			Key: "cert-ingress", Name: "Ingress", Group: model.GroupCert,
			Compare: model.CompareExpiry, Kind: "cert", Version: "",
			Extra: map[string]string{"error": "ingress domain not found"},
		})
	}
	return out, nil
}

func ingressDomain(ctx context.Context, c *Clients) string {
	obj, err := c.Dynamic.Resource(ingressConfigGVR).Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return ""
	}
	d, _, _ := unstructured.NestedString(obj.Object, "spec", "domain")
	return d
}

// expiryComponent dials hostPort (TLS, SNI=serverName) and reports the leaf
// certificate's NotAfter as an RFC3339 timestamp. On failure the value is empty
// (rendered as unknown) with the error recorded in Extra.
func expiryComponent(ctx context.Context, key, name, hostPort, serverName string) model.Component {
	comp := model.Component{
		Key: key, Name: name, Group: model.GroupCert,
		Compare: model.CompareExpiry, Kind: "cert",
	}
	notAfter, err := certNotAfter(ctx, hostPort, serverName)
	if err != nil {
		comp.Extra = map[string]string{"error": err.Error()}
		return comp
	}
	comp.Version = notAfter.UTC().Format(time.RFC3339)
	return comp
}

func certNotAfter(ctx context.Context, hostPort, serverName string) (time.Time, error) {
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	d := &net.Dialer{}
	conn, err := (&tls.Dialer{
		NetDialer: d,
		Config:    &tls.Config{InsecureSkipVerify: true, ServerName: serverName}, //nolint:gosec // reading expiry only
	}).DialContext(dctx, "tcp", hostPort)
	if err != nil {
		return time.Time{}, err
	}
	defer conn.Close()
	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return time.Time{}, fmt.Errorf("no peer certificates")
	}
	return state.PeerCertificates[0].NotAfter, nil
}

// apiHostPort turns an API URL into host:port (default 443).
func apiHostPort(rawURL string) string {
	h, p := splitHostPort(rawURL)
	if p == "" {
		p = "443"
	}
	return net.JoinHostPort(h, p)
}

func hostOnly(rawURL string) string {
	h, _ := splitHostPort(rawURL)
	return h
}

func splitHostPort(rawURL string) (host, port string) {
	if rawURL == "" {
		return "", ""
	}
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		if h, p, e := net.SplitHostPort(u.Host); e == nil {
			return h, p
		}
		return u.Host, ""
	}
	if h, p, e := net.SplitHostPort(rawURL); e == nil {
		return h, p
	}
	return rawURL, ""
}

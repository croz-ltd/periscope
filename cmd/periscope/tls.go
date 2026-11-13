package main

import (
	"crypto/tls"
	"os"
	"sync"
	"time"
)

// certReloader serves the certificate from disk and reads it again when the
// files change. The OpenShift service CA rotates the serving certificate, and
// the kubelet updates the mounted Secret in place, so the hub picks the new one
// up without a restart.
type certReloader struct {
	certFile, keyFile string

	mu      sync.Mutex
	cert    *tls.Certificate
	modTime time.Time
	checked time.Time
}

func newCertReloader(certFile, keyFile string) (*certReloader, error) {
	c := &certReloader{certFile: certFile, keyFile: keyFile}
	if _, err := c.get(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *certReloader) get() (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.cert != nil && now.Sub(c.checked) < time.Minute {
		return c.cert, nil
	}
	c.checked = now
	info, err := os.Stat(c.certFile)
	if err != nil {
		if c.cert != nil {
			return c.cert, nil // keep serving the last good one
		}
		return nil, err
	}
	if c.cert != nil && info.ModTime().Equal(c.modTime) {
		return c.cert, nil
	}
	cert, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		if c.cert != nil {
			return c.cert, nil
		}
		return nil, err
	}
	c.cert, c.modTime = &cert, info.ModTime()
	return c.cert, nil
}

func (c *certReloader) tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return c.get()
		},
	}
}

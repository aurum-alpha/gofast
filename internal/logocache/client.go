package logocache

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// HostPolicy is a per-hostname TLS exception for artwork downloads.
type HostPolicy struct {
	CAPem              string
	InsecureSkipVerify bool
}

// NewArtworkClient builds an HTTP client whose TLS trust uses system roots plus
// optional per-host extras. Hosts not listed use system roots only.
func NewArtworkClient(timeout time.Duration, hosts map[string]HostPolicy) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	policies := make(map[string]resolvedHost, len(hosts))
	for host, p := range hosts {
		key := strings.ToLower(strings.TrimSpace(host))
		if key == "" {
			return nil, fmt.Errorf("logocache: empty artwork TLS hostname")
		}
		resolved := resolvedHost{insecure: p.InsecureSkipVerify}
		if p.CAPem != "" {
			pool, err := systemPoolWithPEM(p.CAPem)
			if err != nil {
				return nil, fmt.Errorf("logocache: artwork TLS %s: %w", key, err)
			}
			resolved.roots = pool
		}
		policies[key] = resolved
	}

	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("logocache: unexpected default transport type")
	}
	transport := base.Clone()
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		cfg := &tls.Config{
			ServerName: host,
			MinVersion: tls.VersionTLS12,
		}
		if p, ok := policies[strings.ToLower(host)]; ok {
			if p.insecure {
				cfg.InsecureSkipVerify = true
			} else if p.roots != nil {
				cfg.RootCAs = p.roots
			}
		}
		d := &net.Dialer{Timeout: 30 * time.Second}
		raw, err := d.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		conn := tls.Client(raw, cfg)
		if err := conn.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		return conn, nil
	}

	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

type resolvedHost struct {
	roots    *x509.CertPool
	insecure bool
}

func systemPoolWithPEM(pemData string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	rest := []byte(pemData)
	added := 0
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		pool.AddCert(cert)
		added++
	}
	if added == 0 {
		return nil, fmt.Errorf("no certificates found")
	}
	return pool, nil
}

package api

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// NewUnixTransport returns an http.Transport that dials the given unix socket
// for all connections, ignoring the request address.
func NewUnixTransport(socketPath string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}
}

// NewTCPTransport returns an http.Transport configured for TCP connections,
// optionally with TLS. If tlsCfg is nil, plain HTTP is used.
func NewTCPTransport(tlsCfg *tls.Config) *http.Transport {
	t := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}
	if tlsCfg != nil {
		t.TLSClientConfig = tlsCfg
	}
	return t
}

// WrapTransport returns the given transport, optionally wrapped with
// AuthTransport if token is non-empty.
func WrapTransport(transport http.RoundTripper, token string) http.RoundTripper {
	if token != "" {
		return &AuthTransport{Base: transport, Token: token}
	}
	return transport
}

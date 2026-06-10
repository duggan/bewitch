package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

// bearerAuth returns an HTTP middleware that requires a valid Bearer token
// in the Authorization header. If token is empty, the handler is returned
// unwrapped (no authentication enforced).
//
// The provided and expected tokens are SHA-256 hashed before the constant-time
// comparison so the comparison is over two equal-length (32-byte) digests. A raw
// subtle.ConstantTimeCompare returns immediately on a length mismatch, which
// leaks the secret's length; hashing first removes that side channel.
func bearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(auth) < len(prefix) || auth[:len(prefix)] != prefix {
			http.Error(w, "missing or malformed Authorization header", http.StatusUnauthorized)
			return
		}
		got := sha256.Sum256([]byte(auth[len(prefix):]))
		if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AuthTransport wraps an http.RoundTripper to inject an Authorization: Bearer
// header on every request. Used by TUI and REPL clients for TCP connections.
type AuthTransport struct {
	Base  http.RoundTripper
	Token string
}

// RoundTrip implements http.RoundTripper.
func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Token != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+t.Token)
	}
	return t.Base.RoundTrip(req)
}

package api

import (
	"crypto/subtle"
	"net/http"
)

// bearerAuth returns an HTTP middleware that requires a valid Bearer token
// in the Authorization header. If token is empty, the handler is returned
// unwrapped (no authentication enforced).
func bearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	tokenBytes := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(auth) < len(prefix) || auth[:len(prefix)] != prefix {
			http.Error(w, "missing or malformed Authorization header", http.StatusUnauthorized)
			return
		}
		provided := []byte(auth[len(prefix):])
		if subtle.ConstantTimeCompare(provided, tokenBytes) != 1 {
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

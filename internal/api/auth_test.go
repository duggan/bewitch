package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerAuth(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		token      string // server token ("" = no auth configured)
		header     string // Authorization header value
		wantStatus int
	}{
		{"no token configured, no header", "", "", 200},
		{"no token configured, header sent", "", "Bearer anything", 200},
		{"valid token", "secret", "Bearer secret", 200},
		{"wrong token", "secret", "Bearer wrong", 401},
		{"missing header", "secret", "", 401},
		{"malformed header (no Bearer prefix)", "secret", "Basic secret", 401},
		{"empty bearer value", "secret", "Bearer ", 401},
		{"same length wrong value", "aaaa", "Bearer bbbb", 401},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := bearerAuth(tt.token, ok)
			req := httptest.NewRequest("GET", "/api/status", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestAuthTransport(t *testing.T) {
	t.Run("injects header when token set", func(t *testing.T) {
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		client := &http.Client{
			Transport: &AuthTransport{Base: http.DefaultTransport, Token: "my-token"},
		}
		resp, err := client.Get(srv.URL + "/api/status")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		resp.Body.Close()

		if gotAuth != "Bearer my-token" {
			t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer my-token")
		}
	})

	t.Run("no header when token empty", func(t *testing.T) {
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		client := &http.Client{
			Transport: &AuthTransport{Base: http.DefaultTransport, Token: ""},
		}
		resp, err := client.Get(srv.URL + "/api/status")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		resp.Body.Close()

		if gotAuth != "" {
			t.Errorf("Authorization = %q, want empty", gotAuth)
		}
	})
}

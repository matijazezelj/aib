package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	tests := []struct {
		header string
		want   string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
	}
	for _, tt := range tests {
		got := rr.Header().Get(tt.header)
		if got != tt.want {
			t.Errorf("%s = %q, want %q", tt.header, got, tt.want)
		}
	}

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header missing")
	}
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP = %q, want to contain default-src 'self'", csp)
	}
}

func TestLimitBody_UnderLimit(t *testing.T) {
	handler := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.NewReader(make([]byte, 1024)) // 1KB, well under 1MB limit
	req := httptest.NewRequest("POST", "/api/v1/scan", body)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestLimitBody_OverLimit(t *testing.T) {
	handler := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.NewReader(make([]byte, 2<<20)) // 2MB, over 1MB limit
	req := httptest.NewRequest("POST", "/api/v1/scan", body)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rr.Code)
	}
}

func TestLimitBody_GET_NoLimit(t *testing.T) {
	handler := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/graph", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestCorsMiddleware_WithOrigin(t *testing.T) {
	s := &Server{corsOrigin: "https://example.com"}
	handler := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/graph", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("CORS origin = %q, want https://example.com", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("CORS methods header missing")
	}
}

func TestCorsMiddleware_NoOrigin(t *testing.T) {
	s := &Server{corsOrigin: ""}
	handler := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/graph", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("CORS origin = %q, want empty (no cors configured)", got)
	}
}

func TestCorsMiddleware_NonAPIPath(t *testing.T) {
	s := &Server{corsOrigin: "https://example.com"}
	handler := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("CORS origin = %q, want empty for non-API path", got)
	}
}

func TestCorsMiddleware_Preflight(t *testing.T) {
	s := &Server{corsOrigin: "https://example.com"}
	handler := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/api/v1/graph", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", rr.Code)
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"/absolute/path", false},
		{"/home/user/state.tfstate", false},
		{"relative/path", true},
		{"../escape", true},
		{"/clean/path/ok", false},
	}
	for _, tt := range tests {
		err := validatePath(tt.path)
		if (err != nil) != tt.wantErr {
			t.Errorf("validatePath(%q) error = %v, wantErr = %v", tt.path, err, tt.wantErr)
		}
	}
}

func TestValidateScanRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     scanTriggerRequest
		wantErr bool
	}{
		{
			name: "valid paths",
			req:  scanTriggerRequest{Paths: []string{"/home/user/state.tfstate"}},
		},
		{
			name:    "relative path in paths",
			req:     scanTriggerRequest{Paths: []string{"../etc/passwd"}},
			wantErr: true,
		},
		{
			name:    "relative path",
			req:     scanTriggerRequest{Paths: []string{"relative/path"}},
			wantErr: true,
		},
		{
			name: "valid namespace",
			req:  scanTriggerRequest{Namespaces: []string{"default", "kube-system"}},
		},
		{
			name:    "invalid namespace",
			req:     scanTriggerRequest{Namespaces: []string{"INVALID"}},
			wantErr: true,
		},
		{
			name:    "namespace with special chars",
			req:     scanTriggerRequest{Namespaces: []string{"ns;drop table"}},
			wantErr: true,
		},
		{
			name: "valid values_file",
			req:  scanTriggerRequest{ValuesFile: "/home/user/values.yaml"},
		},
		{
			name:    "relative values_file",
			req:     scanTriggerRequest{ValuesFile: "../etc/passwd"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScanRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateScanRequest() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuthMiddleware_NoToken(t *testing.T) {
	s := &Server{apiToken: ""}
	handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/graph", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (no token = open)", rr.Code)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	s := &Server{apiToken: "test-token"}
	handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/graph", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	s := &Server{apiToken: "test-token"}
	handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/graph", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestAuthMiddleware_NonAPIPath(t *testing.T) {
	s := &Server{apiToken: "test-token"}
	handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Non-API paths should bypass auth
	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (non-API bypasses auth)", rr.Code)
	}
}

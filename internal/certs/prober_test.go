package certs

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbe_LocalTLS(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Extract host:port from the test server URL
	host, port, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	hostPort := net.JoinHostPort(host, port)

	result, err := Probe(hostPort, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if result.Host != host {
		t.Errorf("Host = %q, want %q", result.Host, host)
	}
	if result.Port != port {
		t.Errorf("Port = %q, want %q", result.Port, port)
	}
	if result.NotAfter.IsZero() {
		t.Error("NotAfter should not be zero")
	}
	if result.NotBefore.IsZero() {
		t.Error("NotBefore should not be zero")
	}
	if result.Serial == "" {
		t.Error("Serial should not be empty")
	}
	if result.Error != "" {
		t.Errorf("Error should be empty, got %q", result.Error)
	}
}

func TestProbe_InvalidHost(t *testing.T) {
	_, err := Probe("invalid-host-that-does-not-exist.local:9999", 2*time.Second)
	if err == nil {
		t.Error("expected error for invalid host")
	}
}

func TestProbe_DefaultPort(t *testing.T) {
	// When no port is specified, it should default to 443
	// This will fail to connect but we can verify the result has port 443
	result, err := Probe("invalid-host-no-port.local", 1*time.Second)
	if err == nil {
		t.Error("expected error for invalid host")
	}
	if result.Port != "443" {
		t.Errorf("Port = %q, want 443 (default)", result.Port)
	}
}

func TestProbe_CertDetails(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hostPort := ts.Listener.Addr().String()

	// Get expected values by inspecting the TLS cert directly
	conn, err := tls.Dial("tcp", hostPort, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	peerCerts := conn.ConnectionState().PeerCertificates
	_ = conn.Close()
	if len(peerCerts) == 0 {
		t.Fatal("no peer certificates")
	}
	wantSerial := peerCerts[0].SerialNumber.String()

	result, err := Probe(hostPort, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Cert should expire in the future
	if !result.NotAfter.After(time.Now()) {
		t.Error("test cert NotAfter should be in the future")
	}

	if result.Serial != wantSerial {
		t.Errorf("Serial = %q, want %q", result.Serial, wantSerial)
	}
}

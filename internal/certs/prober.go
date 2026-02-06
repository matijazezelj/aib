package certs

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"
)

// ProbeResult contains the result of probing a TLS endpoint.
type ProbeResult struct {
	Host       string     `json:"host"`
	Port       string     `json:"port"`
	Subject    string     `json:"subject"`
	Issuer     string     `json:"issuer"`
	NotBefore  time.Time  `json:"not_before"`
	NotAfter   time.Time  `json:"not_after"`
	DNSNames   []string   `json:"dns_names"`
	Serial     string     `json:"serial"`
	Error      string     `json:"error,omitempty"`
}

// Probe connects to a TLS endpoint and inspects the certificate chain.
func Probe(hostPort string, timeout time.Duration) (*ProbeResult, error) {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		host = hostPort
		port = "443"
		hostPort = net.JoinHostPort(host, port)
	}

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", hostPort, &tls.Config{
		InsecureSkipVerify: true, // #nosec G402 -- intentional: probing certs on arbitrary endpoints
	})
	if err != nil {
		return &ProbeResult{
			Host:  host,
			Port:  port,
			Error: err.Error(),
		}, fmt.Errorf("connecting to %s: %w", hostPort, err)
	}
	defer conn.Close() //nolint:errcheck // best-effort cleanup

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return &ProbeResult{
			Host:  host,
			Port:  port,
			Error: "no certificates presented",
		}, fmt.Errorf("no certificates from %s", hostPort)
	}

	leaf := certs[0]
	return &ProbeResult{
		Host:      host,
		Port:      port,
		Subject:   leaf.Subject.CommonName,
		Issuer:    leaf.Issuer.CommonName,
		NotBefore: leaf.NotBefore,
		NotAfter:  leaf.NotAfter,
		DNSNames:  leaf.DNSNames,
		Serial:    leaf.SerialNumber.String(),
	}, nil
}

// DaysUntilExpiry returns the number of days until a certificate expires.
func DaysUntilExpiry(notAfter time.Time) int {
	return int(time.Until(notAfter).Hours() / 24)
}

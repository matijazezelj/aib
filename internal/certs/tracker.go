package certs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/pkg/models"
)

// Tracker manages certificate discovery and expiry tracking.
type Tracker struct {
	store      *graph.SQLiteStore
	thresholds []int
	logger     *slog.Logger
}

// NewTracker creates a new certificate tracker.
func NewTracker(store *graph.SQLiteStore, thresholds []int, logger *slog.Logger) *Tracker {
	if thresholds == nil {
		thresholds = []int{90, 60, 30, 14, 7, 1}
	}
	return &Tracker{
		store:      store,
		thresholds: thresholds,
		logger:     logger,
	}
}

// CertInfo holds certificate information with expiry details.
type CertInfo struct {
	Node          models.Node `json:"node"`
	DaysRemaining int         `json:"days_remaining"`
	Status        string      `json:"status"` // "ok", "warning", "critical", "expired"
}

// ListCerts returns all certificate nodes with expiry info.
func (t *Tracker) ListCerts(ctx context.Context) ([]CertInfo, error) {
	nodes, err := t.store.ListNodes(ctx, graph.NodeFilter{Type: string(models.AssetCertificate)})
	if err != nil {
		return nil, fmt.Errorf("listing certificate nodes: %w", err)
	}

	var certs []CertInfo
	for _, n := range nodes {
		ci := CertInfo{Node: n}
		if n.ExpiresAt != nil {
			ci.DaysRemaining = DaysUntilExpiry(*n.ExpiresAt)
			ci.Status = expiryStatus(ci.DaysRemaining)
		} else {
			ci.Status = "unknown"
			ci.DaysRemaining = -1
		}
		certs = append(certs, ci)
	}
	return certs, nil
}

// ExpiringCerts returns certificates expiring within the given number of days.
func (t *Tracker) ExpiringCerts(ctx context.Context, days int) ([]CertInfo, error) {
	nodes, err := t.store.ExpiringNodes(ctx, days)
	if err != nil {
		return nil, fmt.Errorf("listing expiring nodes: %w", err)
	}

	var certs []CertInfo
	for _, n := range nodes {
		if n.Type != models.AssetCertificate {
			continue
		}
		ci := CertInfo{
			Node:          n,
			DaysRemaining: DaysUntilExpiry(*n.ExpiresAt),
		}
		ci.Status = expiryStatus(ci.DaysRemaining)
		certs = append(certs, ci)
	}
	return certs, nil
}

// ProbeAndStore probes a TLS endpoint and stores the result as a certificate node.
func (t *Tracker) ProbeAndStore(ctx context.Context, hostPort string) (*CertInfo, error) {
	result, err := Probe(hostPort, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("probing %s: %w", hostPort, err)
	}

	now := time.Now()
	nodeID := fmt.Sprintf("probe:certificate:%s", result.Host)

	node := models.Node{
		ID:         nodeID,
		Name:       result.Subject,
		Type:       models.AssetCertificate,
		Source:     "probe",
		SourceFile: hostPort,
		Provider:   result.Issuer,
		ExpiresAt:  &result.NotAfter,
		LastSeen:   now,
		FirstSeen:  now,
		Metadata: map[string]string{
			"host":       result.Host,
			"port":       result.Port,
			"issuer":     result.Issuer,
			"serial":     result.Serial,
			"dns_names":  fmt.Sprintf("%v", result.DNSNames),
			"not_before": result.NotBefore.Format(time.RFC3339),
		},
	}

	if err := t.store.UpsertNode(ctx, node); err != nil {
		return nil, fmt.Errorf("storing certificate: %w", err)
	}

	ci := &CertInfo{
		Node:          node,
		DaysRemaining: DaysUntilExpiry(result.NotAfter),
	}
	ci.Status = expiryStatus(ci.DaysRemaining)

	t.logger.Info("probed certificate",
		"host", hostPort,
		"subject", result.Subject,
		"expires", result.NotAfter.Format("2006-01-02"),
		"days_remaining", ci.DaysRemaining,
	)

	return ci, nil
}

func expiryStatus(days int) string {
	switch {
	case days < 0:
		return "expired"
	case days <= 7:
		return "critical"
	case days <= 30:
		return "warning"
	default:
		return "ok"
	}
}

package certs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matijazezelj/aib/internal/alert"
	"github.com/matijazezelj/aib/internal/graph"
)

// CertScheduler periodically probes TLS endpoints and sends alerts.
type CertScheduler struct {
	tracker  *Tracker
	store    *graph.SQLiteStore
	alerter  alert.Alerter
	interval time.Duration
	logger   *slog.Logger
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewCertScheduler creates a scheduler that probes certs on the given interval.
// The interval string is parsed with time.ParseDuration (e.g. "6h", "30m").
func NewCertScheduler(tracker *Tracker, store *graph.SQLiteStore, alerter alert.Alerter, interval string, logger *slog.Logger) (*CertScheduler, error) {
	d, err := time.ParseDuration(interval)
	if err != nil {
		return nil, fmt.Errorf("invalid cert probe interval %q: %w", interval, err)
	}
	if d < 1*time.Minute {
		return nil, fmt.Errorf("cert probe interval must be at least 1m, got %s", d)
	}
	return &CertScheduler{
		tracker:  tracker,
		store:    store,
		alerter:  alerter,
		interval: d,
		logger:   logger,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}, nil
}

// Start begins the periodic probing loop. Call Stop() to terminate.
func (cs *CertScheduler) Start(ctx context.Context) {
	go func() {
		defer close(cs.doneCh)
		ticker := time.NewTicker(cs.interval)
		defer ticker.Stop()

		cs.logger.Info("cert probe scheduler started", "interval", cs.interval.String())

		for {
			select {
			case <-ticker.C:
				cs.logger.Info("starting scheduled cert probe")
				results := ProbeAll(ctx, cs.tracker, cs.store, cs.logger)
				cs.sendAlerts(ctx, results)
			case <-cs.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop halts the scheduler and waits for it to finish.
func (cs *CertScheduler) Stop() {
	close(cs.stopCh)
	<-cs.doneCh
}

func (cs *CertScheduler) sendAlerts(ctx context.Context, results []CertInfo) {
	if cs.alerter == nil {
		return
	}
	for _, ci := range results {
		if ci.Status == "warning" || ci.Status == "critical" || ci.Status == "expired" {
			event := alert.Event{
				Source:    "aib",
				EventType: "cert_expiring",
				Severity:  ci.Status,
				Asset: alert.Asset{
					ID:            ci.Node.ID,
					Name:          ci.Node.Name,
					Type:          string(ci.Node.Type),
					DaysRemaining: ci.DaysRemaining,
				},
				Message:   fmt.Sprintf("Certificate %s expires in %d days", ci.Node.Name, ci.DaysRemaining),
				Timestamp: time.Now(),
			}
			if ci.Node.ExpiresAt != nil {
				event.Asset.ExpiresAt = ci.Node.ExpiresAt.Format(time.RFC3339)
			}
			if err := cs.alerter.Send(ctx, event); err != nil {
				cs.logger.Warn("failed to send cert alert", "cert", ci.Node.Name, "error", err)
			}
		}
	}
}

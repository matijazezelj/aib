package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Scheduler runs scans periodically using a time.Ticker.
type Scheduler struct {
	scanner  *Scanner
	interval time.Duration
	logger   *slog.Logger
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewScheduler creates a scheduler. The interval string is parsed with
// time.ParseDuration (e.g. "4h", "30m", "1h30m").
func NewScheduler(sc *Scanner, interval string, logger *slog.Logger) (*Scheduler, error) {
	d, err := time.ParseDuration(interval)
	if err != nil {
		return nil, fmt.Errorf("invalid scan schedule %q: %w (use Go duration format: 4h, 30m, etc.)", interval, err)
	}
	if d < 1*time.Minute {
		return nil, fmt.Errorf("scan interval must be at least 1m, got %s", d)
	}
	return &Scheduler{
		scanner:  sc,
		interval: d,
		logger:   logger,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}, nil
}

// Start begins the scheduling loop. Call Stop() to terminate.
func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		defer close(s.doneCh)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.logger.Info("scan scheduler started", "interval", s.interval.String())

		for {
			select {
			case <-ticker.C:
				if s.scanner.IsRunning() {
					s.logger.Info("skipping scheduled scan, previous scan still running")
					continue
				}
				s.logger.Info("starting scheduled scan")
				results := s.scanner.RunAllConfigured(ctx)
				for _, r := range results {
					if r.Error != nil {
						s.logger.Error("scheduled scan failed", "scanID", r.ScanID, "error", r.Error)
					} else {
						s.logger.Info("scheduled scan completed",
							"scanID", r.ScanID, "nodes", r.NodesFound, "edges", r.EdgesFound)
					}
				}
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop halts the scheduler and waits for it to finish.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	<-s.doneCh
}

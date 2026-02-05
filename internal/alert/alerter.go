package alert

import (
	"context"
	"time"
)

// Event represents an alert event sent to alerting backends.
type Event struct {
	Source    string    `json:"source"`
	EventType string   `json:"event_type"`
	Severity  string   `json:"severity"`
	Asset     Asset     `json:"asset"`
	Impact    *Impact   `json:"impact,omitempty"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// Asset is the asset info embedded in an alert event.
type Asset struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	DaysRemaining int    `json:"days_remaining,omitempty"`
}

// Impact describes the blast radius impact in an alert.
type Impact struct {
	AffectedCount    int      `json:"affected_count"`
	AffectedServices []string `json:"affected_services"`
}

// Alerter defines the interface for sending alert events.
type Alerter interface {
	// Name returns the alerter identifier.
	Name() string

	// Send dispatches an event to the alerting backend.
	Send(ctx context.Context, event Event) error
}

// Multi sends events to multiple alerters.
type Multi struct {
	alerters []Alerter
}

// NewMulti creates a multi-alerter that dispatches to all backends.
func NewMulti(alerters ...Alerter) *Multi {
	return &Multi{alerters: alerters}
}

// Send dispatches the event to all configured alerters.
func (m *Multi) Send(ctx context.Context, event Event) error {
	var lastErr error
	for _, a := range m.alerters {
		if err := a.Send(ctx, event); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

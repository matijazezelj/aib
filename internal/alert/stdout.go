package alert

import (
	"context"
	"fmt"
	"time"
)

// StdoutAlerter prints events to stdout.
type StdoutAlerter struct{}

func NewStdoutAlerter() *StdoutAlerter {
	return &StdoutAlerter{}
}

func (s *StdoutAlerter) Name() string {
	return "stdout"
}

func (s *StdoutAlerter) Send(_ context.Context, event Event) error {
	icon := severityIcon(event.Severity)
	ts := event.Timestamp.Format(time.RFC3339)

	fmt.Printf("%s [%s] %s %s — %s\n", icon, ts, event.EventType, event.Asset.ID, event.Message)

	if event.Impact != nil && event.Impact.AffectedCount > 0 {
		fmt.Printf("   Impact: %d affected assets\n", event.Impact.AffectedCount)
	}

	return nil
}

func severityIcon(severity string) string {
	switch severity {
	case "critical":
		return "[CRIT]"
	case "warning":
		return "[WARN]"
	case "info":
		return "[INFO]"
	default:
		return "[----]"
	}
}

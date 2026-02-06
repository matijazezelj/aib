package scanner

import (
	"log/slog"
	"os"
	"testing"
)

func TestNewScheduler_ValidDuration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		interval string
		wantErr  bool
	}{
		{"4h", false},
		{"30m", false},
		{"1h30m", false},
		{"2m", false},
		{"30s", true},  // below 1m minimum
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.interval, func(t *testing.T) {
			_, err := NewScheduler(nil, tt.interval, logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewScheduler(%q) error = %v, wantErr %v", tt.interval, err, tt.wantErr)
			}
		})
	}
}

package certs

import (
	"log/slog"
	"os"
	"testing"
)

func TestNewCertScheduler_ValidDuration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		interval string
		wantErr  bool
	}{
		{"6h", false},
		{"30m", false},
		{"1h30m", false},
		{"2m", false},
		{"30s", true},  // below 1m minimum
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.interval, func(t *testing.T) {
			_, err := NewCertScheduler(nil, nil, nil, tt.interval, logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCertScheduler(%q) error = %v, wantErr %v", tt.interval, err, tt.wantErr)
			}
		})
	}
}

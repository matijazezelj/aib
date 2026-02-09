package certs

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/matijazezelj/aib/internal/alert"
	"github.com/matijazezelj/aib/pkg/models"
)

type mockAlerter struct {
	mu     sync.Mutex
	events []alert.Event
}

func (m *mockAlerter) Name() string { return "mock" }

func (m *mockAlerter) Send(_ context.Context, event alert.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockAlerter) getEvents() []alert.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]alert.Event, len(m.events))
	copy(cp, m.events)
	return cp
}

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

func TestCertScheduler_StartStop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cs, err := NewCertScheduler(nil, nil, nil, "1m", logger)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cs.Start(ctx)

	// Immediately stop — should not deadlock
	cancel()
	done := make(chan struct{})
	go func() {
		cs.Stop()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("CertScheduler.Stop() deadlocked")
	}
}

func TestCertScheduler_ContextCancel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cs, err := NewCertScheduler(nil, nil, nil, "1m", logger)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cs.Start(ctx)

	// Cancel the context — doneCh should close
	cancel()

	select {
	case <-cs.doneCh:
		// ok: goroutine exited
	case <-time.After(5 * time.Second):
		t.Fatal("CertScheduler did not exit on context cancel")
	}
}

func TestCertScheduler_SendAlerts_Warning(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mock := &mockAlerter{}

	cs, err := NewCertScheduler(nil, nil, mock, "1m", logger)
	if err != nil {
		t.Fatal(err)
	}

	expires := time.Now().Add(10 * 24 * time.Hour)
	results := []CertInfo{
		{
			Node:          models.Node{ID: "cert:test", Name: "test-cert", Type: models.AssetCertificate, ExpiresAt: &expires},
			DaysRemaining: 10,
			Status:        "warning",
		},
	}

	cs.sendAlerts(context.Background(), results)

	events := mock.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(events))
	}
	if events[0].Severity != "warning" {
		t.Errorf("severity = %q, want warning", events[0].Severity)
	}
}

func TestCertScheduler_SendAlerts_OK(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mock := &mockAlerter{}

	cs, err := NewCertScheduler(nil, nil, mock, "1m", logger)
	if err != nil {
		t.Fatal(err)
	}

	expires := time.Now().Add(90 * 24 * time.Hour)
	results := []CertInfo{
		{
			Node:          models.Node{ID: "cert:ok", Name: "ok-cert", Type: models.AssetCertificate, ExpiresAt: &expires},
			DaysRemaining: 90,
			Status:        "ok",
		},
	}

	cs.sendAlerts(context.Background(), results)

	events := mock.getEvents()
	if len(events) != 0 {
		t.Errorf("expected 0 alerts for ok cert, got %d", len(events))
	}
}

func TestCertScheduler_SendAlerts_NilAlerter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cs, err := NewCertScheduler(nil, nil, nil, "1m", logger)
	if err != nil {
		t.Fatal(err)
	}

	// Should not panic with nil alerter
	results := []CertInfo{
		{
			Node:          models.Node{ID: "cert:nil", Name: "nil-cert"},
			DaysRemaining: 5,
			Status:        "warning",
		},
	}

	cs.sendAlerts(context.Background(), results)
}

func TestCertScheduler_SendAlerts_Expired(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mock := &mockAlerter{}

	cs, err := NewCertScheduler(nil, nil, mock, "1m", logger)
	if err != nil {
		t.Fatal(err)
	}

	expired := time.Now().Add(-24 * time.Hour)
	results := []CertInfo{
		{
			Node:          models.Node{ID: "cert:expired", Name: "expired-cert", Type: models.AssetCertificate, ExpiresAt: &expired},
			DaysRemaining: -1,
			Status:        "expired",
		},
	}

	cs.sendAlerts(context.Background(), results)

	events := mock.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 alert for expired cert, got %d", len(events))
	}
	if events[0].Severity != "expired" {
		t.Errorf("severity = %q, want expired", events[0].Severity)
	}
}

func TestCertScheduler_SendAlerts_WithExpiry(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mock := &mockAlerter{}

	cs, err := NewCertScheduler(nil, nil, mock, "1m", logger)
	if err != nil {
		t.Fatal(err)
	}

	expires := time.Now().Add(3 * 24 * time.Hour)
	results := []CertInfo{
		{
			Node:          models.Node{ID: "cert:exp", Name: "exp-cert", Type: models.AssetCertificate, ExpiresAt: &expires},
			DaysRemaining: 3,
			Status:        "critical",
		},
	}

	cs.sendAlerts(context.Background(), results)

	events := mock.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(events))
	}
	if events[0].Asset.ExpiresAt == "" {
		t.Error("expected ExpiresAt to be set in alert event")
	}
	if events[0].Severity != "critical" {
		t.Errorf("severity = %q, want critical", events[0].Severity)
	}
}

package certs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/pkg/models"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *graph.SQLiteStore {
	t.Helper()
	dbPath := t.TempDir() + "/test.db"
	store, err := graph.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedCertNode(t *testing.T, store *graph.SQLiteStore, id, name string, expiresAt *time.Time) {
	t.Helper()
	now := time.Now()
	node := models.Node{
		ID: id, Name: name, Type: models.AssetCertificate,
		Source: "test", Provider: "test",
		Metadata:  map[string]string{},
		ExpiresAt: expiresAt,
		LastSeen:  now, FirstSeen: now,
	}
	if err := store.UpsertNode(context.Background(), node); err != nil {
		t.Fatal(err)
	}
}

func TestDaysUntilExpiry(t *testing.T) {
	tests := []struct {
		name     string
		notAfter time.Time
		wantMin  int
		wantMax  int
	}{
		{"future_30d", time.Now().Add(30 * 24 * time.Hour), 29, 31},
		{"future_1d", time.Now().Add(24 * time.Hour), 0, 2},
		{"past", time.Now().Add(-24 * time.Hour), -2, 0},
		{"today", time.Now(), -1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DaysUntilExpiry(tt.notAfter)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("DaysUntilExpiry() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestExpiryStatus(t *testing.T) {
	tests := []struct {
		days int
		want string
	}{
		{-1, "expired"},
		{0, "critical"},
		{5, "critical"},
		{7, "critical"},
		{8, "warning"},
		{30, "warning"},
		{31, "ok"},
		{365, "ok"},
	}
	for _, tt := range tests {
		got := expiryStatus(tt.days)
		if got != tt.want {
			t.Errorf("expiryStatus(%d) = %q, want %q", tt.days, got, tt.want)
		}
	}
}

func TestListCerts(t *testing.T) {
	store := newTestStore(t)
	logger := newNopLogger()
	tracker := NewTracker(store, nil, logger)

	// Seed cert nodes
	future := time.Now().Add(60 * 24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)
	seedCertNode(t, store, "cert:ok", "ok-cert", &future)
	seedCertNode(t, store, "cert:expired", "expired-cert", &past)
	seedCertNode(t, store, "cert:unknown", "unknown-cert", nil)

	// Seed a non-cert node to verify filtering
	now := time.Now()
	_ = store.UpsertNode(context.Background(), models.Node{
		ID: "vm:web1", Name: "web1", Type: models.AssetVM,
		Source: "test", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	})

	certs, err := tracker.ListCerts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 3 {
		t.Fatalf("expected 3 certs, got %d", len(certs))
	}

	statusMap := make(map[string]string)
	for _, c := range certs {
		statusMap[c.Node.ID] = c.Status
	}

	if statusMap["cert:ok"] != "ok" {
		t.Errorf("cert:ok status = %q, want ok", statusMap["cert:ok"])
	}
	if statusMap["cert:expired"] != "expired" {
		t.Errorf("cert:expired status = %q, want expired", statusMap["cert:expired"])
	}
	if statusMap["cert:unknown"] != "unknown" {
		t.Errorf("cert:unknown status = %q, want unknown", statusMap["cert:unknown"])
	}
}

func TestExpiringCerts(t *testing.T) {
	store := newTestStore(t)
	logger := newNopLogger()
	tracker := NewTracker(store, nil, logger)

	soon := time.Now().Add(10 * 24 * time.Hour)
	far := time.Now().Add(120 * 24 * time.Hour)
	seedCertNode(t, store, "cert:soon", "soon-cert", &soon)
	seedCertNode(t, store, "cert:far", "far-cert", &far)

	certs, err := tracker.ExpiringCerts(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 expiring cert, got %d", len(certs))
	}
	if certs[0].Node.ID != "cert:soon" {
		t.Errorf("expected cert:soon, got %s", certs[0].Node.ID)
	}
}

func newNopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

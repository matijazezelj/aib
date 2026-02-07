package scanner

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matijazezelj/aib/internal/config"
	"github.com/matijazezelj/aib/internal/graph"
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

func newTestScanner(t *testing.T) (*Scanner, *graph.SQLiteStore) {
	t.Helper()
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}
	return New(store, cfg, logger), store
}

func TestRunSync_Terraform(t *testing.T) {
	sc, store := newTestScanner(t)

	// Use the existing testdata file
	testdata, err := filepath.Abs("../parser/terraform/testdata/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(testdata); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", testdata)
	}

	result := sc.RunSync(context.Background(), ScanRequest{
		Source: "terraform",
		Paths:  []string{testdata},
	})

	if result.Error != nil {
		t.Fatalf("RunSync error: %v", result.Error)
	}
	if result.ScanID <= 0 {
		t.Error("expected positive scan ID")
	}
	if result.NodesFound == 0 {
		t.Error("expected nodes to be found")
	}

	// Verify scan record
	scans, err := store.ListScans(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 {
		t.Fatalf("expected 1 scan record, got %d", len(scans))
	}
	if scans[0].Status != "completed" {
		t.Errorf("scan status = %q, want completed", scans[0].Status)
	}
	if scans[0].NodesFound != result.NodesFound {
		t.Errorf("scan nodes = %d, result nodes = %d", scans[0].NodesFound, result.NodesFound)
	}
}

func TestRunSync_InvalidPath(t *testing.T) {
	sc, store := newTestScanner(t)

	result := sc.RunSync(context.Background(), ScanRequest{
		Source: "terraform",
		Paths:  []string{"/nonexistent/path/state.tfstate"},
	})

	if result.Error == nil {
		t.Error("expected error for invalid path")
	}

	scans, _ := store.ListScans(context.Background(), 10)
	if len(scans) != 1 {
		t.Fatalf("expected 1 scan, got %d", len(scans))
	}
	if scans[0].Status != "failed" {
		t.Errorf("scan status = %q, want failed", scans[0].Status)
	}
}

func TestRunSync_UnknownSource(t *testing.T) {
	sc, _ := newTestScanner(t)

	result := sc.RunSync(context.Background(), ScanRequest{
		Source: "unknown",
		Paths:  []string{"/some/path"},
	})

	if result.Error == nil {
		t.Error("expected error for unknown source")
	}
}

func TestIsRunning(t *testing.T) {
	sc, _ := newTestScanner(t)

	if sc.IsRunning() {
		t.Error("scanner should not be running initially")
	}
}

func TestRunAsync_Terraform(t *testing.T) {
	sc, store := newTestScanner(t)

	testdata, err := filepath.Abs("../parser/terraform/testdata/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(testdata); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", testdata)
	}

	scanID, err := sc.RunAsync(context.Background(), ScanRequest{
		Source: "terraform",
		Paths:  []string{testdata},
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanID <= 0 {
		t.Error("expected positive scan ID")
	}

	// Wait for async scan to complete (poll with sleep)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 100; i++ {
		scans, _ := store.ListScans(ctx, 10)
		if len(scans) > 0 && scans[0].Status != "running" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for async scan to complete")
		case <-time.After(50 * time.Millisecond):
		}
	}

	scans, _ := store.ListScans(ctx, 10)
	found := false
	for _, s := range scans {
		if s.ID == scanID {
			found = true
			if s.Status != "completed" {
				t.Errorf("async scan status = %q, want completed", s.Status)
			}
		}
	}
	if !found {
		t.Error("scan record not found")
	}
}

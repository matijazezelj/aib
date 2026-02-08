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

func TestRunSync_TerraformPlan(t *testing.T) {
	sc, store := newTestScanner(t)

	testdata, err := filepath.Abs("../parser/terraform/testdata/plan_create.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(testdata); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", testdata)
	}

	result := sc.RunSync(context.Background(), ScanRequest{
		Source: "terraform-plan",
		Paths:  []string{testdata},
	})

	if result.Error != nil {
		t.Fatalf("RunSync error: %v", result.Error)
	}
	if result.NodesFound != 2 {
		t.Errorf("NodesFound = %d, want 2", result.NodesFound)
	}

	scans, _ := store.ListScans(context.Background(), 10)
	if len(scans) != 1 {
		t.Fatalf("expected 1 scan, got %d", len(scans))
	}
	if scans[0].Status != "completed" {
		t.Errorf("scan status = %q, want completed", scans[0].Status)
	}
}

func TestRunSync_TerraformPlanRealistic(t *testing.T) {
	sc, store := newTestScanner(t)
	ctx := context.Background()

	testdata, err := filepath.Abs("../parser/terraform/testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(testdata); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", testdata)
	}

	result := sc.RunSync(ctx, ScanRequest{
		Source: "terraform-plan",
		Paths:  []string{testdata},
	})

	if result.Error != nil {
		t.Fatalf("RunSync error: %v", result.Error)
	}
	if result.NodesFound != 11 {
		t.Errorf("NodesFound = %d, want 11", result.NodesFound)
	}
	if result.EdgesFound < 4 {
		t.Errorf("EdgesFound = %d, want >= 4 (vpc_id attribute edges)", result.EdgesFound)
	}

	// Verify nodes were persisted to store
	nodes, err := store.ListNodes(ctx, graph.NodeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 11 {
		t.Errorf("stored nodes = %d, want 11", len(nodes))
	}

	// Verify edges were persisted
	edges, err := store.ListEdges(ctx, graph.EdgeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) < 4 {
		t.Errorf("stored edges = %d, want >= 4", len(edges))
	}

	// Verify specific node exists in store with correct metadata
	node, err := store.GetNode(ctx, "tf:vm:web-server")
	if err != nil {
		t.Fatal(err)
	}
	if node == nil {
		t.Fatal("tf:vm:web-server not found in store")
	}
	if node.Source != "terraform-plan" {
		t.Errorf("stored source = %q, want terraform-plan", node.Source)
	}
	if node.Metadata["plan_action"] != "create" {
		t.Errorf("stored plan_action = %q, want create", node.Metadata["plan_action"])
	}

	// Verify scan record
	scans, _ := store.ListScans(ctx, 10)
	if len(scans) != 1 {
		t.Fatalf("expected 1 scan, got %d", len(scans))
	}
	if scans[0].Status != "completed" {
		t.Errorf("scan status = %q, want completed", scans[0].Status)
	}
	if scans[0].NodesFound != 11 {
		t.Errorf("scan record NodesFound = %d, want 11", scans[0].NodesFound)
	}
}

func TestRunSync_TerraformPlanMultiFile(t *testing.T) {
	sc, store := newTestScanner(t)
	ctx := context.Background()

	infra, err := filepath.Abs("../parser/terraform/testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}
	services, err := filepath.Abs("../parser/terraform/testdata/plan_services.json")
	if err != nil {
		t.Fatal(err)
	}

	result := sc.RunSync(ctx, ScanRequest{
		Source: "terraform-plan",
		Paths:  []string{infra, services},
	})

	if result.Error != nil {
		t.Fatalf("RunSync error: %v", result.Error)
	}
	// plan_realistic: 11 nodes, plan_services: 6 nodes = 17 total
	if result.NodesFound != 17 {
		t.Errorf("NodesFound = %d, want 17", result.NodesFound)
	}

	// Verify all nodes persisted
	nodes, err := store.ListNodes(ctx, graph.NodeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 17 {
		t.Errorf("stored nodes = %d, want 17", len(nodes))
	}

	// Verify cross-file edge: api-service → prod-vpc
	edges, err := store.ListEdges(ctx, graph.EdgeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	foundCrossFileEdge := false
	for _, e := range edges {
		if e.FromID == "tf:service:api-service" && e.ToID == "tf:network:prod-vpc" {
			foundCrossFileEdge = true
			break
		}
	}
	if !foundCrossFileEdge {
		t.Error("missing cross-file edge: api-service → prod-vpc")
	}
}

package scanner

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matijazezelj/aib/internal/config"
	"github.com/matijazezelj/aib/internal/graph"
)

// Integration tests exercise real parsers + real SQLite + query + export.

func newIntegrationScanner(t *testing.T) (*Scanner, *graph.SQLiteStore, graph.GraphEngine) {
	t.Helper()
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}
	sc := New(store, cfg, logger)
	engine := graph.NewLocalEngine(store)
	return sc, store, engine
}

func TestIntegration_Terraform_ScanQueryExport(t *testing.T) {
	testdata, err := filepath.Abs("../parser/terraform/testdata/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(testdata); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", testdata)
	}

	sc, store, engine := newIntegrationScanner(t)
	ctx := context.Background()

	// Step 1: Scan
	result := sc.RunSync(ctx, ScanRequest{
		Source: "terraform",
		Paths:  []string{testdata},
	})
	if result.Error != nil {
		t.Fatalf("scan error: %v", result.Error)
	}
	if result.NodesFound == 0 {
		t.Fatal("expected nodes from terraform scan")
	}

	// Step 2: Verify node/edge counts
	nodeCount, _ := store.NodeCount(ctx)
	edgeCount, _ := store.EdgeCount(ctx)
	if nodeCount != result.NodesFound {
		t.Errorf("NodeCount = %d, scan reported %d", nodeCount, result.NodesFound)
	}
	if edgeCount != result.EdgesFound {
		t.Errorf("EdgeCount = %d, scan reported %d", edgeCount, result.EdgesFound)
	}

	// Step 3: Query — list all nodes
	nodes, _ := store.ListNodes(ctx, graph.NodeFilter{})
	if len(nodes) == 0 {
		t.Fatal("no nodes in store")
	}

	// Step 4: BlastRadius on first node
	br, err := engine.BlastRadius(ctx, nodes[0].ID)
	if err != nil {
		t.Fatalf("BlastRadius error: %v", err)
	}
	if br.Root != nodes[0].ID {
		t.Errorf("root = %s, want %s", br.Root, nodes[0].ID)
	}

	// Step 5: Export JSON
	jsonOut, err := graph.ExportJSON(ctx, store)
	if err != nil {
		t.Fatalf("ExportJSON error: %v", err)
	}
	var data graph.GraphData
	if err := json.Unmarshal([]byte(jsonOut), &data); err != nil {
		t.Fatalf("ExportJSON invalid JSON: %v", err)
	}
	if len(data.Nodes) != nodeCount {
		t.Errorf("JSON nodes = %d, want %d", len(data.Nodes), nodeCount)
	}

	// Step 6: Export DOT
	dotOut, err := graph.ExportDOT(ctx, store)
	if err != nil {
		t.Fatalf("ExportDOT error: %v", err)
	}
	if !strings.Contains(dotOut, "digraph aib") {
		t.Error("DOT output should contain 'digraph aib'")
	}

	// Step 7: Export Mermaid
	mermaidOut, err := graph.ExportMermaid(ctx, store)
	if err != nil {
		t.Fatalf("ExportMermaid error: %v", err)
	}
	if !strings.Contains(mermaidOut, "graph LR") {
		t.Error("Mermaid output should contain 'graph LR'")
	}
}

func TestIntegration_Compose_ScanAndQuery(t *testing.T) {
	testdata, err := filepath.Abs("../parser/compose/testdata/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(testdata); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", testdata)
	}

	sc, store, engine := newIntegrationScanner(t)
	ctx := context.Background()

	result := sc.RunSync(ctx, ScanRequest{
		Source: "compose",
		Paths:  []string{testdata},
	})
	if result.Error != nil {
		t.Fatalf("scan error: %v", result.Error)
	}
	if result.NodesFound == 0 {
		t.Fatal("expected nodes from compose scan")
	}

	// Verify neighbors query
	nodes, _ := store.ListNodes(ctx, graph.NodeFilter{})
	for _, n := range nodes {
		neighbors, err := engine.Neighbors(ctx, n.ID)
		if err != nil {
			t.Fatalf("Neighbors(%s) error: %v", n.ID, err)
		}
		// Just verify no error; not all nodes have neighbors
		_ = neighbors
	}

	// Try shortest path between first and last node
	if len(nodes) >= 2 {
		_, _, err := engine.ShortestPath(ctx, nodes[0].ID, nodes[len(nodes)-1].ID)
		// Path may or may not exist, just verify no panic
		_ = err
	}
}

func TestIntegration_Ansible_ScanAndQuery(t *testing.T) {
	invPath, err := filepath.Abs("../parser/ansible/testdata/inventory.ini")
	if err != nil {
		t.Fatal(err)
	}
	pbPath, err := filepath.Abs("../parser/ansible/testdata/playbook.yml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(invPath); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", invPath)
	}

	sc, store, engine := newIntegrationScanner(t)
	ctx := context.Background()

	result := sc.RunSync(ctx, ScanRequest{
		Source:    "ansible",
		Paths:     []string{invPath},
		Playbooks: filepath.Dir(pbPath),
	})
	if result.Error != nil {
		t.Fatalf("scan error: %v", result.Error)
	}
	if result.NodesFound == 0 {
		t.Fatal("expected nodes from ansible scan")
	}

	// Dependency chain on first node
	nodes, _ := store.ListNodes(ctx, graph.NodeFilter{})
	if len(nodes) > 0 {
		deps, err := engine.DependencyChain(ctx, nodes[0].ID, 10)
		if err != nil {
			t.Fatalf("DependencyChain error: %v", err)
		}
		_ = deps
	}
}

func TestIntegration_RunAllConfigured(t *testing.T) {
	tfPath, err := filepath.Abs("../parser/terraform/testdata/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	composePath, err := filepath.Abs("../parser/compose/testdata/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	invPath, err := filepath.Abs("../parser/ansible/testdata/inventory.ini")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tfPath); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", tfPath)
	}
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", composePath)
	}
	if _, err := os.Stat(invPath); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", invPath)
	}

	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{
		Sources: config.SourcesConfig{
			Terraform: []config.TerraformSource{
				{StateFile: tfPath},
			},
			Compose: []config.ComposeSource{
				{Path: composePath},
			},
			Ansible: []config.AnsibleSource{
				{Inventory: invPath},
			},
		},
	}
	sc := New(store, cfg, logger)
	ctx := context.Background()

	results := sc.RunAllConfigured(ctx)
	if len(results) != 3 {
		t.Fatalf("expected 3 results from RunAllConfigured, got %d", len(results))
	}

	for i, r := range results {
		if r.Error != nil {
			t.Errorf("result[%d] error: %v", i, r.Error)
		}
		if r.NodesFound == 0 {
			t.Errorf("result[%d] found 0 nodes", i)
		}
	}
}

func TestIntegration_RunAllConfigured_Empty(t *testing.T) {
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{} // no sources configured
	sc := New(store, cfg, logger)

	results := sc.RunAllConfigured(context.Background())
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestIntegration_RunAllConfigured_EmptyPaths(t *testing.T) {
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{
		Sources: config.SourcesConfig{
			Terraform: []config.TerraformSource{
				{}, // empty path and state_file, should be skipped
			},
			Ansible: []config.AnsibleSource{
				{}, // empty inventory, should be skipped
			},
			Compose: []config.ComposeSource{
				{}, // empty path, should be skipped
			},
		},
	}
	sc := New(store, cfg, logger)

	results := sc.RunAllConfigured(context.Background())
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty paths, got %d", len(results))
	}
}

func TestIntegration_MultiSource(t *testing.T) {
	tfPath, err := filepath.Abs("../parser/terraform/testdata/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	composePath, err := filepath.Abs("../parser/compose/testdata/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tfPath); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", tfPath)
	}
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", composePath)
	}

	sc, store, _ := newIntegrationScanner(t)
	ctx := context.Background()

	// Scan terraform
	r1 := sc.RunSync(ctx, ScanRequest{
		Source: "terraform",
		Paths:  []string{tfPath},
	})
	if r1.Error != nil {
		t.Fatalf("terraform scan error: %v", r1.Error)
	}

	// Scan compose
	r2 := sc.RunSync(ctx, ScanRequest{
		Source: "compose",
		Paths:  []string{composePath},
	})
	if r2.Error != nil {
		t.Fatalf("compose scan error: %v", r2.Error)
	}

	// Both should coexist
	nodeCount, _ := store.NodeCount(ctx)
	if nodeCount < r1.NodesFound+r2.NodesFound {
		t.Errorf("total nodes %d < sum of scans (%d + %d)", nodeCount, r1.NodesFound, r2.NodesFound)
	}

	// Cross-source queries should work
	nodes, _ := store.ListNodes(ctx, graph.NodeFilter{Source: "terraform"})
	if len(nodes) == 0 {
		t.Error("expected terraform nodes")
	}
	nodes, _ = store.ListNodes(ctx, graph.NodeFilter{Source: "compose"})
	if len(nodes) == 0 {
		t.Error("expected compose nodes")
	}

	// Verify scan records
	scans, _ := store.ListScans(ctx, 10)
	if len(scans) != 2 {
		t.Errorf("expected 2 scan records, got %d", len(scans))
	}
}

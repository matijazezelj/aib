package graph

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/matijazezelj/aib/pkg/models"
)

func TestExportJSON(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	nodes := []models.Node{
		makeNode("n1", models.AssetVM, "terraform"),
		makeNode("n2", models.AssetNetwork, "terraform"),
		makeNode("n3", models.AssetDatabase, "terraform"),
	}
	edges := []models.Edge{
		makeEdge("n1", "n2", models.EdgeDependsOn),
		makeEdge("n1", "n3", models.EdgeConnectsTo),
	}
	buildTestGraph(t, store, nodes, edges)

	out, err := ExportJSON(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	var data GraphData
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if len(data.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(data.Nodes))
	}
	if len(data.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(data.Edges))
	}
}

func TestExportJSON_Empty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	out, err := ExportJSON(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	var data GraphData
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if len(data.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(data.Nodes))
	}
	if len(data.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(data.Edges))
	}
}

func TestExportDOT(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	nodes := []models.Node{
		makeNode("n1", models.AssetVM, "terraform"),
		makeNode("n2", models.AssetNetwork, "terraform"),
	}
	edges := []models.Edge{
		makeEdge("n1", "n2", models.EdgeDependsOn),
	}
	buildTestGraph(t, store, nodes, edges)

	out, err := ExportDOT(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "digraph aib") {
		t.Error("DOT output missing 'digraph aib'")
	}
	if !strings.Contains(out, `"n1"`) {
		t.Error("DOT output missing node n1")
	}
	if !strings.Contains(out, `"n1" -> "n2"`) {
		t.Error("DOT output missing edge n1 -> n2")
	}
}

func TestExportDOT_Empty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	out, err := ExportDOT(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "digraph aib") {
		t.Error("DOT output missing 'digraph aib'")
	}
}

func TestExportMermaid(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	nodes := []models.Node{
		makeNode("n1", models.AssetVM, "terraform"),
		makeNode("n2", models.AssetNetwork, "terraform"),
	}
	edges := []models.Edge{
		makeEdge("n1", "n2", models.EdgeDependsOn),
	}
	buildTestGraph(t, store, nodes, edges)

	out, err := ExportMermaid(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "graph LR") {
		t.Error("Mermaid output missing 'graph LR'")
	}
	if !strings.Contains(out, "n1") {
		t.Error("Mermaid output missing node n1")
	}
	if !strings.Contains(out, "depends_on") {
		t.Error("Mermaid output missing edge type")
	}
}

func TestExportMermaid_Empty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	out, err := ExportMermaid(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "graph LR") {
		t.Error("Mermaid output missing 'graph LR'")
	}
}

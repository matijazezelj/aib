package graph

import (
	"context"
	"testing"

	"github.com/matijazezelj/aib/pkg/models"
)

// buildLinearGraph creates A->B->C (A depends on B, B depends on C).
func buildLinearGraph(t *testing.T) (*SQLiteStore, *LocalEngine) {
	t.Helper()
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetNetwork, "tf"),
			makeNode("C", models.AssetSubnet, "tf"),
		},
		[]models.Edge{
			makeEdge("A", "B", models.EdgeDependsOn),
			makeEdge("B", "C", models.EdgeDependsOn),
		},
	)
	return store, NewLocalEngine(store)
}

func TestBlastRadius_Linear(t *testing.T) {
	_, engine := buildLinearGraph(t)
	ctx := context.Background()

	// If C fails, B and A are affected (they depend on C transitively)
	result, err := engine.BlastRadius(ctx, "C")
	if err != nil {
		t.Fatal(err)
	}
	if result.AffectedNodes != 2 {
		t.Errorf("AffectedNodes = %d, want 2", result.AffectedNodes)
	}
	if _, ok := result.ImpactTree["B"]; !ok {
		t.Error("B should be in impact tree")
	}
	if _, ok := result.ImpactTree["A"]; !ok {
		t.Error("A should be in impact tree")
	}
}

func TestBlastRadius_Diamond(t *testing.T) {
	store := newTestStore(t)
	// A->C, B->C, A->D, B->D (diamond shape)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetVM, "tf"),
			makeNode("C", models.AssetNetwork, "tf"),
			makeNode("D", models.AssetSubnet, "tf"),
		},
		[]models.Edge{
			makeEdge("A", "C", models.EdgeDependsOn),
			makeEdge("B", "C", models.EdgeDependsOn),
			makeEdge("A", "D", models.EdgeDependsOn),
			makeEdge("B", "D", models.EdgeDependsOn),
		},
	)
	engine := NewLocalEngine(store)

	result, _ := engine.BlastRadius(context.Background(), "C")
	if result.AffectedNodes != 2 {
		t.Errorf("AffectedNodes = %d, want 2 (A and B)", result.AffectedNodes)
	}
}

func TestBlastRadius_Isolated(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store, []models.Node{makeNode("X", models.AssetVM, "tf")}, nil)
	engine := NewLocalEngine(store)

	result, _ := engine.BlastRadius(context.Background(), "X")
	if result.AffectedNodes != 0 {
		t.Errorf("AffectedNodes = %d, want 0", result.AffectedNodes)
	}
}

func TestBlastRadiusTree_Linear(t *testing.T) {
	_, engine := buildLinearGraph(t)

	tree, err := engine.BlastRadiusTree(context.Background(), "C")
	if err != nil {
		t.Fatal(err)
	}
	if tree.NodeID != "C" {
		t.Errorf("root = %s, want C", tree.NodeID)
	}
	if len(tree.Children) != 1 {
		t.Fatalf("root children = %d, want 1 (B)", len(tree.Children))
	}
	if tree.Children[0].NodeID != "B" {
		t.Errorf("child = %s, want B", tree.Children[0].NodeID)
	}
	if len(tree.Children[0].Children) != 1 {
		t.Fatalf("B children = %d, want 1 (A)", len(tree.Children[0].Children))
	}
	if tree.Children[0].Children[0].NodeID != "A" {
		t.Errorf("grandchild = %s, want A", tree.Children[0].Children[0].NodeID)
	}
}

func TestBlastRadiusTree_Fan(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetVM, "tf"),
			makeNode("C", models.AssetVM, "tf"),
			makeNode("D", models.AssetNetwork, "tf"),
		},
		[]models.Edge{
			makeEdge("A", "D", models.EdgeDependsOn),
			makeEdge("B", "D", models.EdgeDependsOn),
			makeEdge("C", "D", models.EdgeDependsOn),
		},
	)
	engine := NewLocalEngine(store)

	tree, _ := engine.BlastRadiusTree(context.Background(), "D")
	if len(tree.Children) != 3 {
		t.Errorf("fan children = %d, want 3", len(tree.Children))
	}
}

func TestNeighbors(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetNetwork, "tf"),
			makeNode("C", models.AssetSubnet, "tf"),
		},
		[]models.Edge{
			makeEdge("A", "B", models.EdgeDependsOn),
			makeEdge("C", "A", models.EdgeConnectsTo),
		},
	)
	engine := NewLocalEngine(store)

	neighbors, _ := engine.Neighbors(context.Background(), "A")
	if len(neighbors) != 2 {
		t.Errorf("neighbors = %d, want 2", len(neighbors))
	}
}

func TestShortestPath_Direct(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetNetwork, "tf"),
		},
		[]models.Edge{makeEdge("A", "B", models.EdgeDependsOn)},
	)
	engine := NewLocalEngine(store)

	nodes, _, err := engine.ShortestPath(context.Background(), "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Errorf("path length = %d, want 2", len(nodes))
	}
}

func TestShortestPath_TwoHops(t *testing.T) {
	_, engine := buildLinearGraph(t)

	nodes, _, err := engine.ShortestPath(context.Background(), "A", "C")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Errorf("path length = %d, want 3", len(nodes))
	}
}

func TestShortestPath_NoPath(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetVM, "tf"),
		},
		nil, // no edges = disconnected
	)
	engine := NewLocalEngine(store)

	_, _, err := engine.ShortestPath(context.Background(), "A", "B")
	if err == nil {
		t.Error("expected error for disconnected nodes")
	}
}

func TestDependencyChain_Linear(t *testing.T) {
	_, engine := buildLinearGraph(t)

	deps, _ := engine.DependencyChain(context.Background(), "A", 10)
	if len(deps) != 2 {
		t.Errorf("deps = %d, want 2 (B, C)", len(deps))
	}
}

func TestDependencyChain_MaxDepth(t *testing.T) {
	_, engine := buildLinearGraph(t)

	deps, _ := engine.DependencyChain(context.Background(), "A", 1)
	if len(deps) != 1 {
		t.Errorf("deps with maxDepth=1: got %d, want 1 (B only)", len(deps))
	}
}

func TestDependencyChain_Cycle(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetNetwork, "tf"),
			makeNode("C", models.AssetSubnet, "tf"),
		},
		[]models.Edge{
			makeEdge("A", "B", models.EdgeDependsOn),
			makeEdge("B", "C", models.EdgeDependsOn),
			makeEdge("C", "A", models.EdgeDependsOn), // cycle
		},
	)
	engine := NewLocalEngine(store)

	// Should terminate without infinite loop
	deps, err := engine.DependencyChain(context.Background(), "A", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 {
		t.Errorf("deps = %d, want 2 (B, C - cycle does not revisit A)", len(deps))
	}
}

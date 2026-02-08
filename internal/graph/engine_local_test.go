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

// --- FindCycles tests ---

func TestFindCycles_WithCycle(t *testing.T) {
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

	cycles, err := engine.FindCycles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 1 {
		t.Fatalf("cycles = %d, want 1", len(cycles))
	}
	if len(cycles[0]) != 3 {
		t.Errorf("cycle length = %d, want 3", len(cycles[0]))
	}
	// Normalized: starts with smallest ID ("A")
	if cycles[0][0] != "A" {
		t.Errorf("cycle[0] = %s, want A (normalized)", cycles[0][0])
	}
}

func TestFindCycles_NoCycle(t *testing.T) {
	_, engine := buildLinearGraph(t)

	cycles, err := engine.FindCycles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 0 {
		t.Errorf("cycles = %d, want 0", len(cycles))
	}
}

func TestFindCycles_MultipleCycles(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetNetwork, "tf"),
			makeNode("C", models.AssetSubnet, "tf"),
			makeNode("D", models.AssetVM, "tf"),
			makeNode("E", models.AssetNetwork, "tf"),
		},
		[]models.Edge{
			makeEdge("A", "B", models.EdgeDependsOn),
			makeEdge("B", "A", models.EdgeDependsOn), // cycle 1: A<->B
			makeEdge("D", "E", models.EdgeDependsOn),
			makeEdge("E", "D", models.EdgeDependsOn), // cycle 2: D<->E
		},
	)
	engine := NewLocalEngine(store)

	cycles, err := engine.FindCycles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 2 {
		t.Errorf("cycles = %d, want 2", len(cycles))
	}
}

func TestFindCycles_EmptyGraph(t *testing.T) {
	store := newTestStore(t)
	engine := NewLocalEngine(store)

	cycles, err := engine.FindCycles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 0 {
		t.Errorf("cycles = %d, want 0", len(cycles))
	}
}

// --- FindSPOF tests ---

func TestFindSPOF_HubNode(t *testing.T) {
	store := newTestStore(t)
	// Hub: C is depended on by A, B, and D
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetVM, "tf"),
			makeNode("C", models.AssetNetwork, "tf"),
			makeNode("D", models.AssetVM, "tf"),
		},
		[]models.Edge{
			makeEdge("A", "C", models.EdgeDependsOn),
			makeEdge("B", "C", models.EdgeDependsOn),
			makeEdge("D", "C", models.EdgeDependsOn),
		},
	)
	engine := NewLocalEngine(store)

	spofs, err := engine.FindSPOF(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(spofs) == 0 {
		t.Fatal("expected at least one SPOF")
	}
	// C should be the top SPOF with 3 affected
	if spofs[0].Node.ID != "C" {
		t.Errorf("top SPOF = %s, want C", spofs[0].Node.ID)
	}
	if spofs[0].AffectedCount != 3 {
		t.Errorf("affected = %d, want 3", spofs[0].AffectedCount)
	}
}

func TestFindSPOF_ThresholdFilter(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetNetwork, "tf"),
		},
		[]models.Edge{
			makeEdge("A", "B", models.EdgeDependsOn),
		},
	)
	engine := NewLocalEngine(store)

	// minAffected=2 should filter out B (only 1 affected)
	spofs, err := engine.FindSPOF(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(spofs) != 0 {
		t.Errorf("spofs = %d, want 0 (threshold=2)", len(spofs))
	}
}

func TestFindSPOF_EmptyGraph(t *testing.T) {
	store := newTestStore(t)
	engine := NewLocalEngine(store)

	spofs, err := engine.FindSPOF(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(spofs) != 0 {
		t.Errorf("spofs = %d, want 0", len(spofs))
	}
}

// --- FindOrphans tests ---

func TestFindOrphans_MixedGraph(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetNetwork, "tf"),
			makeNode("C", models.AssetSubnet, "tf"), // orphan
		},
		[]models.Edge{
			makeEdge("A", "B", models.EdgeDependsOn),
		},
	)
	engine := NewLocalEngine(store)

	orphans, err := engine.FindOrphans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 {
		t.Fatalf("orphans = %d, want 1", len(orphans))
	}
	if orphans[0].ID != "C" {
		t.Errorf("orphan = %s, want C", orphans[0].ID)
	}
}

func TestFindOrphans_AllConnected(t *testing.T) {
	_, engine := buildLinearGraph(t)

	orphans, err := engine.FindOrphans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Errorf("orphans = %d, want 0", len(orphans))
	}
}

func TestFindOrphans_AllOrphans(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetNetwork, "tf"),
		},
		nil, // no edges
	)
	engine := NewLocalEngine(store)

	orphans, err := engine.FindOrphans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 2 {
		t.Errorf("orphans = %d, want 2", len(orphans))
	}
}

// --- normalizeCycle tests ---

func TestNormalizeCycle(t *testing.T) {
	tests := []struct {
		input []string
		want  string
	}{
		{[]string{"C", "A", "B"}, "A"},
		{[]string{"A", "B", "C"}, "A"},
		{[]string{"B", "C", "A"}, "A"},
		{[]string{}, ""},
	}
	for _, tt := range tests {
		got := normalizeCycle(tt.input)
		if len(tt.input) == 0 {
			if len(got) != 0 {
				t.Errorf("normalizeCycle(%v) = %v, want empty", tt.input, got)
			}
			continue
		}
		if got[0] != tt.want {
			t.Errorf("normalizeCycle(%v)[0] = %s, want %s", tt.input, got[0], tt.want)
		}
	}
}

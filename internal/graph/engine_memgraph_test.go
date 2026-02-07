package graph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// --- Pure function tests (no mocking) ---

func TestToString(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{nil, ""},
		{"hello", "hello"},
		{42, "42"},
		{int64(99), "99"},
		{3.14, "3.14"},
	}
	for _, tt := range tests {
		got := toString(tt.input)
		if got != tt.want {
			t.Errorf("toString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetRecordString(t *testing.T) {
	rec := &neo4j.Record{
		Keys:   []string{"name", "age", "empty"},
		Values: []any{"alice", 30, nil},
	}

	if got := getRecordString(rec, "name"); got != "alice" {
		t.Errorf("getRecordString(name) = %q, want %q", got, "alice")
	}
	if got := getRecordString(rec, "age"); got != "30" {
		t.Errorf("getRecordString(age) = %q, want %q", got, "30")
	}
	if got := getRecordString(rec, "empty"); got != "" {
		t.Errorf("getRecordString(empty) = %q, want %q", got, "")
	}
	if got := getRecordString(rec, "missing"); got != "" {
		t.Errorf("getRecordString(missing) = %q, want %q", got, "")
	}
}

func TestRecordToNode_Full(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	expires := now.Add(30 * 24 * time.Hour)

	rec := &neo4j.Record{
		Keys: []string{"id", "name", "type", "source", "source_file", "provider", "metadata", "expires_at", "last_seen", "first_seen"},
		Values: []any{
			"vm:web1", "web1", "vm", "terraform", "main.tf", "gcp",
			`{"region":"us-east1"}`,
			expires.Format(time.RFC3339),
			now.Format(time.RFC3339),
			now.Format(time.RFC3339),
		},
	}

	node := recordToNode(rec)
	if node.ID != "vm:web1" {
		t.Errorf("ID = %q", node.ID)
	}
	if node.Name != "web1" {
		t.Errorf("Name = %q", node.Name)
	}
	if node.Type != models.AssetVM {
		t.Errorf("Type = %q", node.Type)
	}
	if node.Source != "terraform" {
		t.Errorf("Source = %q", node.Source)
	}
	if node.SourceFile != "main.tf" {
		t.Errorf("SourceFile = %q", node.SourceFile)
	}
	if node.Provider != "gcp" {
		t.Errorf("Provider = %q", node.Provider)
	}
	if node.Metadata["region"] != "us-east1" {
		t.Errorf("Metadata[region] = %q", node.Metadata["region"])
	}
	if node.ExpiresAt == nil {
		t.Fatal("ExpiresAt should not be nil")
	}
	if !node.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", node.ExpiresAt, expires)
	}
}

func TestRecordToNode_Minimal(t *testing.T) {
	rec := &neo4j.Record{
		Keys:   []string{"id", "name", "type", "source", "source_file", "provider", "metadata", "expires_at", "last_seen", "first_seen"},
		Values: []any{"a", "node-a", "vm", "tf", nil, nil, nil, nil, nil, nil},
	}

	node := recordToNode(rec)
	if node.ID != "a" {
		t.Errorf("ID = %q", node.ID)
	}
	if node.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil for empty value")
	}
}

func TestRecordToNode_BadJSON(t *testing.T) {
	rec := &neo4j.Record{
		Keys:   []string{"id", "name", "type", "source", "source_file", "provider", "metadata", "expires_at", "last_seen", "first_seen"},
		Values: []any{"a", "a", "vm", "tf", "", "", "not-json", "", "", ""},
	}

	node := recordToNode(rec)
	if node.ID != "a" {
		t.Errorf("ID = %q", node.ID)
	}
	// Bad JSON metadata should not crash, just empty map
	if len(node.Metadata) != 0 {
		t.Errorf("Metadata should be empty for bad JSON, got %v", node.Metadata)
	}
}

func TestRecordToNode_BadTimestamp(t *testing.T) {
	rec := &neo4j.Record{
		Keys:   []string{"id", "name", "type", "source", "source_file", "provider", "metadata", "expires_at", "last_seen", "first_seen"},
		Values: []any{"a", "a", "vm", "tf", "", "", "", "bad-time", "bad-time", "bad-time"},
	}

	node := recordToNode(rec)
	if node.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil for bad timestamp")
	}
	if !node.LastSeen.IsZero() {
		t.Error("LastSeen should be zero for bad timestamp")
	}
}

func TestBuildMgTree_NoUpstream(t *testing.T) {
	parent := &ImpactNode{NodeID: "root"}
	upstream := map[string][]mgEdgeInfo{}
	nodeMap := map[string]*models.Node{}
	visited := map[string]bool{"root": true}

	buildMgTree(parent, upstream, nodeMap, visited, 0)
	if len(parent.Children) != 0 {
		t.Errorf("expected 0 children, got %d", len(parent.Children))
	}
}

func TestBuildMgTree_SingleChild(t *testing.T) {
	parent := &ImpactNode{NodeID: "root"}
	upstream := map[string][]mgEdgeInfo{
		"root": {{fromID: "child1", edgeType: models.EdgeDependsOn}},
	}
	nodeMap := map[string]*models.Node{
		"child1": {ID: "child1", Name: "child1"},
	}
	visited := map[string]bool{"root": true}

	buildMgTree(parent, upstream, nodeMap, visited, 0)
	if len(parent.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(parent.Children))
	}
	if parent.Children[0].NodeID != "child1" {
		t.Errorf("child ID = %s", parent.Children[0].NodeID)
	}
	if parent.Children[0].Depth != 1 {
		t.Errorf("child depth = %d, want 1", parent.Children[0].Depth)
	}
}

func TestBuildMgTree_CycleProtection(t *testing.T) {
	parent := &ImpactNode{NodeID: "A"}
	upstream := map[string][]mgEdgeInfo{
		"A": {{fromID: "B", edgeType: models.EdgeDependsOn}},
		"B": {{fromID: "A", edgeType: models.EdgeDependsOn}}, // cycle
	}
	nodeMap := map[string]*models.Node{
		"A": {ID: "A"}, "B": {ID: "B"},
	}
	visited := map[string]bool{"A": true}

	buildMgTree(parent, upstream, nodeMap, visited, 0)
	if len(parent.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(parent.Children))
	}
	// B should not re-add A
	if len(parent.Children[0].Children) != 0 {
		t.Errorf("expected 0 grandchildren (cycle), got %d", len(parent.Children[0].Children))
	}
}

// --- Engine method tests ---

func newTestMemgraphEngine(t *testing.T, sess *mockSession) (*MemgraphEngine, *LocalEngine) {
	t.Helper()
	store := newTestStore(t)
	// Seed with A->B->C linear graph
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
	local := NewLocalEngine(store)
	engine := &MemgraphEngine{
		newSession: mockSessionFactory(sess),
		fallback:   local,
		logger:     slog.New(slog.NewTextHandler(nopWriter{}, nil)),
	}
	return engine, local
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestMemgraph_BlastRadius_Success(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return &mockResult{
				records: []*neo4j.Record{
					makeNodeRecord("B", "B", "network", "tf"),
					makeNodeRecord("A", "A", "vm", "tf"),
				},
			}, nil
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	result, err := engine.BlastRadius(context.Background(), "C")
	if err != nil {
		t.Fatal(err)
	}
	if result.AffectedNodes != 2 {
		t.Errorf("AffectedNodes = %d, want 2", result.AffectedNodes)
	}
	if _, ok := result.ImpactTree["A"]; !ok {
		t.Error("A should be in impact tree")
	}
	if _, ok := result.ImpactTree["B"]; !ok {
		t.Error("B should be in impact tree")
	}
}

func TestMemgraph_BlastRadius_Fallback(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return nil, fmt.Errorf("memgraph down")
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	result, err := engine.BlastRadius(context.Background(), "C")
	if err != nil {
		t.Fatal(err)
	}
	// Fallback to local should still return correct results
	if result.AffectedNodes != 2 {
		t.Errorf("AffectedNodes (fallback) = %d, want 2", result.AffectedNodes)
	}
}

func TestMemgraph_BlastRadius_ResultError(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return &mockResult{err: fmt.Errorf("result error")}, nil
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	result, err := engine.BlastRadius(context.Background(), "C")
	if err != nil {
		t.Fatal(err)
	}
	// Should fallback
	if result.AffectedNodes != 2 {
		t.Errorf("AffectedNodes (result error fallback) = %d, want 2", result.AffectedNodes)
	}
}

func TestMemgraph_BlastRadiusTree_Success(t *testing.T) {
	callCount := 0
	sess := &mockSession{
		runFunc: func(cypher string, _ map[string]any) (resultIterator, error) {
			callCount++
			switch {
			case callCount == 1: // root node query
				return &mockResult{
					records: []*neo4j.Record{
						makeNodeRecord("C", "C", "subnet", "tf"),
					},
				}, nil
			case callCount == 2: // affected nodes query
				return &mockResult{
					records: []*neo4j.Record{
						makeNodeRecord("B", "B", "network", "tf"),
						makeNodeRecord("A", "A", "vm", "tf"),
					},
				}, nil
			case callCount == 3: // edges query
				return &mockResult{
					records: []*neo4j.Record{
						makeRecord(map[string]any{"from_id": "A", "edge_type": "depends_on", "to_id": "B"}),
						makeRecord(map[string]any{"from_id": "B", "edge_type": "depends_on", "to_id": "C"}),
					},
				}, nil
			default:
				return &mockResult{}, nil
			}
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

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
}

func TestMemgraph_BlastRadiusTree_Fallback(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return nil, fmt.Errorf("memgraph down")
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	tree, err := engine.BlastRadiusTree(context.Background(), "C")
	if err != nil {
		t.Fatal(err)
	}
	if tree.NodeID != "C" {
		t.Errorf("root = %s, want C", tree.NodeID)
	}
}

func TestMemgraph_Neighbors_Success(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return &mockResult{
				records: []*neo4j.Record{
					makeNodeRecord("B", "B", "network", "tf"),
					makeNodeRecord("C", "C", "subnet", "tf"),
				},
			}, nil
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	nodes, err := engine.Neighbors(context.Background(), "B")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Errorf("neighbors = %d, want 2", len(nodes))
	}
}

func TestMemgraph_Neighbors_Fallback(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return nil, fmt.Errorf("memgraph down")
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	nodes, err := engine.Neighbors(context.Background(), "A")
	if err != nil {
		t.Fatal(err)
	}
	// A has neighbors B (from edge A->B)
	if len(nodes) != 1 {
		t.Errorf("neighbors (fallback) = %d, want 1", len(nodes))
	}
}

func TestMemgraph_Neighbors_ResultError(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return &mockResult{err: fmt.Errorf("result error")}, nil
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	nodes, err := engine.Neighbors(context.Background(), "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Errorf("neighbors (result error fallback) = %d, want 1", len(nodes))
	}
}

func TestMemgraph_ShortestPath_Success(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return &mockResult{
				records: []*neo4j.Record{
					makeNodeRecord("A", "A", "vm", "tf"),
					makeNodeRecord("B", "B", "network", "tf"),
					makeNodeRecord("C", "C", "subnet", "tf"),
				},
			}, nil
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	nodes, _, err := engine.ShortestPath(context.Background(), "A", "C")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Errorf("path length = %d, want 3", len(nodes))
	}
}

func TestMemgraph_ShortestPath_NoPath(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return &mockResult{records: nil}, nil
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	_, _, err := engine.ShortestPath(context.Background(), "A", "Z")
	if err == nil {
		t.Error("expected error for no path")
	}
	if !strings.Contains(err.Error(), "no path found") {
		t.Errorf("error = %q, want 'no path found'", err.Error())
	}
}

func TestMemgraph_ShortestPath_Fallback(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return nil, fmt.Errorf("memgraph down")
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	nodes, _, err := engine.ShortestPath(context.Background(), "A", "C")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Errorf("path length (fallback) = %d, want 3", len(nodes))
	}
}

func TestMemgraph_DependencyChain_Success(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return &mockResult{
				records: []*neo4j.Record{
					makeNodeRecord("B", "B", "network", "tf"),
					makeNodeRecord("C", "C", "subnet", "tf"),
				},
			}, nil
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	deps, err := engine.DependencyChain(context.Background(), "A", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 {
		t.Errorf("deps = %d, want 2", len(deps))
	}
}

func TestMemgraph_DependencyChain_Fallback(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return nil, fmt.Errorf("memgraph down")
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	deps, err := engine.DependencyChain(context.Background(), "A", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 {
		t.Errorf("deps (fallback) = %d, want 2", len(deps))
	}
}

func TestMemgraph_DependencyChain_ResultError(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return &mockResult{err: fmt.Errorf("result error")}, nil
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	deps, err := engine.DependencyChain(context.Background(), "A", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 {
		t.Errorf("deps (result error fallback) = %d, want 2", len(deps))
	}
}

func TestMemgraph_DependencyChain_MaxDepthDefault(t *testing.T) {
	var capturedCypher string
	sess := &mockSession{
		runFunc: func(cypher string, _ map[string]any) (resultIterator, error) {
			capturedCypher = cypher
			return &mockResult{}, nil
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	// maxDepth=0 should default to 50
	_, _ = engine.DependencyChain(context.Background(), "A", 0)
	if !strings.Contains(capturedCypher, "50") {
		t.Errorf("cypher should contain maxDepth 50 for default, got: %s", capturedCypher)
	}

	// maxDepth=-1 should default to 50
	_, _ = engine.DependencyChain(context.Background(), "A", -1)
	if !strings.Contains(capturedCypher, "50") {
		t.Errorf("cypher should contain maxDepth 50 for negative, got: %s", capturedCypher)
	}

	// maxDepth=999 should default to 50
	_, _ = engine.DependencyChain(context.Background(), "A", 999)
	if !strings.Contains(capturedCypher, "50") {
		t.Errorf("cypher should contain maxDepth 50 for >50, got: %s", capturedCypher)
	}
}

func TestMemgraph_SessionClosed(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return &mockResult{}, nil
		},
	}
	engine, _ := newTestMemgraphEngine(t, sess)

	_, _ = engine.BlastRadius(context.Background(), "C")
	if !sess.closed {
		t.Error("session should be closed after BlastRadius")
	}
}

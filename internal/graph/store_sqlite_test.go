package graph

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{db: db}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func makeNode(id string, typ models.AssetType, source string) models.Node {
	now := time.Now().Truncate(time.Second)
	return models.Node{
		ID: id, Name: id, Type: typ, Source: source,
		Provider: "test", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	}
}

func makeEdge(from, to string, typ models.EdgeType) models.Edge {
	return models.Edge{
		ID:       GenerateEdgeID(from, to, typ),
		FromID:   from,
		ToID:     to,
		Type:     typ,
		Metadata: map[string]string{},
	}
}

func buildTestGraph(t *testing.T, store *SQLiteStore, nodes []models.Node, edges []models.Edge) {
	t.Helper()
	ctx := context.Background()
	for _, n := range nodes {
		if err := store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("inserting node %s: %v", n.ID, err)
		}
	}
	for _, e := range edges {
		if err := store.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("inserting edge %s: %v", e.ID, err)
		}
	}
}

func TestUpsertAndGetNode(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	expires := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	node := models.Node{
		ID: "test:vm:web1", Name: "web1", Type: models.AssetVM,
		Source: "terraform", SourceFile: "main.tf", Provider: "gcp",
		Metadata:  map[string]string{"region": "us-east1", "zone": "a"},
		ExpiresAt: &expires,
		LastSeen:  time.Now().Truncate(time.Second),
		FirstSeen: time.Now().Truncate(time.Second),
	}

	if err := store.UpsertNode(ctx, node); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetNode(ctx, "test:vm:web1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected node, got nil")
	}
	if got.ID != node.ID {
		t.Errorf("ID = %q, want %q", got.ID, node.ID)
	}
	if got.Name != "web1" {
		t.Errorf("Name = %q, want %q", got.Name, "web1")
	}
	if got.Type != models.AssetVM {
		t.Errorf("Type = %q, want %q", got.Type, models.AssetVM)
	}
	if got.Source != "terraform" {
		t.Errorf("Source = %q, want %q", got.Source, "terraform")
	}
	if got.SourceFile != "main.tf" {
		t.Errorf("SourceFile = %q, want %q", got.SourceFile, "main.tf")
	}
	if got.Provider != "gcp" {
		t.Errorf("Provider = %q, want %q", got.Provider, "gcp")
	}
	if got.Metadata["region"] != "us-east1" {
		t.Errorf("Metadata[region] = %q, want %q", got.Metadata["region"], "us-east1")
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt should not be nil")
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
}

func TestUpsertNodeUpdate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	firstSeen := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	node := models.Node{
		ID: "test:vm:web1", Name: "web1", Type: models.AssetVM,
		Source: "terraform", Metadata: map[string]string{},
		LastSeen: firstSeen, FirstSeen: firstSeen,
	}
	_ = store.UpsertNode(ctx, node)

	// Update with new name and last_seen
	node.Name = "web1-updated"
	node.LastSeen = time.Now().Truncate(time.Second)
	_ = store.UpsertNode(ctx, node)

	got, _ := store.GetNode(ctx, "test:vm:web1")
	if got.Name != "web1-updated" {
		t.Errorf("Name = %q, want %q", got.Name, "web1-updated")
	}
	// first_seen should be preserved (not updated by ON CONFLICT)
	if !got.FirstSeen.Equal(firstSeen) {
		t.Errorf("FirstSeen = %v, want %v (should be preserved)", got.FirstSeen, firstSeen)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	got, err := store.GetNode(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestUpsertAndGetEdge(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	buildTestGraph(t, store,
		[]models.Node{
			makeNode("a", models.AssetVM, "tf"),
			makeNode("b", models.AssetNetwork, "tf"),
		},
		nil,
	)

	edge := models.Edge{
		ID: "a->depends_on->b", FromID: "a", ToID: "b",
		Type:     models.EdgeDependsOn,
		Metadata: map[string]string{"via": "network"},
	}
	if err := store.UpsertEdge(ctx, edge); err != nil {
		t.Fatal(err)
	}

	edges, err := store.ListEdges(ctx, EdgeFilter{FromID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].FromID != "a" || edges[0].ToID != "b" {
		t.Errorf("edge = %s -> %s, want a -> b", edges[0].FromID, edges[0].ToID)
	}
	if edges[0].Metadata["via"] != "network" {
		t.Errorf("metadata[via] = %q, want %q", edges[0].Metadata["via"], "network")
	}
}

func TestUpsertEdgeConflict(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	buildTestGraph(t, store,
		[]models.Node{
			makeNode("a", models.AssetVM, "tf"),
			makeNode("b", models.AssetNetwork, "tf"),
		},
		nil,
	)

	edge := models.Edge{
		ID: "e1", FromID: "a", ToID: "b", Type: models.EdgeDependsOn,
		Metadata: map[string]string{"v": "1"},
	}
	_ = store.UpsertEdge(ctx, edge)

	// Upsert with updated metadata (same from_id, to_id, type)
	edge.Metadata = map[string]string{"v": "2"}
	_ = store.UpsertEdge(ctx, edge)

	edges, _ := store.ListEdges(ctx, EdgeFilter{})
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge (upsert), got %d", len(edges))
	}
	if edges[0].Metadata["v"] != "2" {
		t.Errorf("metadata[v] = %q, want %q", edges[0].Metadata["v"], "2")
	}
}

func TestListNodesNoFilter(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store, []models.Node{
		makeNode("a", models.AssetVM, "tf"),
		makeNode("b", models.AssetDatabase, "tf"),
		makeNode("c", models.AssetNetwork, "tf"),
	}, nil)

	nodes, err := store.ListNodes(context.Background(), NodeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}
}

func TestListNodesFilterByType(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store, []models.Node{
		makeNode("a", models.AssetVM, "tf"),
		makeNode("b", models.AssetVM, "tf"),
		makeNode("c", models.AssetDatabase, "tf"),
	}, nil)

	nodes, _ := store.ListNodes(context.Background(), NodeFilter{Type: "vm"})
	if len(nodes) != 2 {
		t.Errorf("expected 2 VMs, got %d", len(nodes))
	}
}

func TestListNodesFilterBySource(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store, []models.Node{
		makeNode("a", models.AssetVM, "terraform"),
		makeNode("b", models.AssetVM, "ansible"),
	}, nil)

	nodes, _ := store.ListNodes(context.Background(), NodeFilter{Source: "ansible"})
	if len(nodes) != 1 {
		t.Errorf("expected 1, got %d", len(nodes))
	}
	if nodes[0].ID != "b" {
		t.Errorf("expected node b, got %s", nodes[0].ID)
	}
}

func TestListNodesFilterByProvider(t *testing.T) {
	store := newTestStore(t)
	n1 := makeNode("a", models.AssetVM, "tf")
	n1.Provider = "gcp"
	n2 := makeNode("b", models.AssetVM, "tf")
	n2.Provider = "aws"
	buildTestGraph(t, store, []models.Node{n1, n2}, nil)

	nodes, _ := store.ListNodes(context.Background(), NodeFilter{Provider: "aws"})
	if len(nodes) != 1 {
		t.Errorf("expected 1, got %d", len(nodes))
	}
}

func TestListEdgesFilters(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("a", models.AssetVM, "tf"),
			makeNode("b", models.AssetNetwork, "tf"),
			makeNode("c", models.AssetSubnet, "tf"),
		},
		[]models.Edge{
			makeEdge("a", "b", models.EdgeDependsOn),
			makeEdge("a", "c", models.EdgeConnectsTo),
			makeEdge("c", "b", models.EdgeDependsOn),
		},
	)
	ctx := context.Background()

	// Filter by type
	edges, _ := store.ListEdges(ctx, EdgeFilter{Type: "depends_on"})
	if len(edges) != 2 {
		t.Errorf("type filter: expected 2, got %d", len(edges))
	}

	// Filter by from
	edges, _ = store.ListEdges(ctx, EdgeFilter{FromID: "a"})
	if len(edges) != 2 {
		t.Errorf("from filter: expected 2, got %d", len(edges))
	}

	// Filter by to
	edges, _ = store.ListEdges(ctx, EdgeFilter{ToID: "b"})
	if len(edges) != 2 {
		t.Errorf("to filter: expected 2, got %d", len(edges))
	}
}

func TestGetNeighbors(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("a", models.AssetVM, "tf"),
			makeNode("b", models.AssetNetwork, "tf"),
			makeNode("c", models.AssetSubnet, "tf"),
		},
		[]models.Edge{
			makeEdge("a", "b", models.EdgeDependsOn),
			makeEdge("c", "a", models.EdgeConnectsTo),
		},
	)

	neighbors, err := store.GetNeighbors(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 2 {
		t.Errorf("expected 2 neighbors, got %d", len(neighbors))
	}
}

func TestGetNeighborsIsolated(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store, []models.Node{makeNode("a", models.AssetVM, "tf")}, nil)

	neighbors, err := store.GetNeighbors(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 0 {
		t.Errorf("expected 0 neighbors, got %d", len(neighbors))
	}
}

func TestGetEdgesFromTo(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("a", models.AssetVM, "tf"),
			makeNode("b", models.AssetNetwork, "tf"),
			makeNode("c", models.AssetSubnet, "tf"),
		},
		[]models.Edge{
			makeEdge("a", "b", models.EdgeDependsOn),
			makeEdge("a", "c", models.EdgeConnectsTo),
			makeEdge("c", "b", models.EdgeDependsOn),
		},
	)
	ctx := context.Background()

	from, _ := store.GetEdgesFrom(ctx, "a")
	if len(from) != 2 {
		t.Errorf("GetEdgesFrom(a): expected 2, got %d", len(from))
	}

	to, _ := store.GetEdgesTo(ctx, "b")
	if len(to) != 2 {
		t.Errorf("GetEdgesTo(b): expected 2, got %d", len(to))
	}
}

func TestDeleteNode(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	buildTestGraph(t, store,
		[]models.Node{
			makeNode("a", models.AssetVM, "tf"),
			makeNode("b", models.AssetNetwork, "tf"),
		},
		[]models.Edge{makeEdge("a", "b", models.EdgeDependsOn)},
	)

	_ = store.DeleteNode(ctx, "a")

	got, _ := store.GetNode(ctx, "a")
	if got != nil {
		t.Error("node should be deleted")
	}

	// Edge should be cascade-deleted
	edges, _ := store.ListEdges(ctx, EdgeFilter{})
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after cascade delete, got %d", len(edges))
	}
}

func TestNodeAndEdgeCount(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	buildTestGraph(t, store,
		[]models.Node{
			makeNode("a", models.AssetVM, "tf"),
			makeNode("b", models.AssetVM, "tf"),
			makeNode("c", models.AssetDatabase, "tf"),
		},
		[]models.Edge{
			makeEdge("a", "b", models.EdgeDependsOn),
			makeEdge("b", "c", models.EdgeConnectsTo),
		},
	)

	nc, _ := store.NodeCount(ctx)
	if nc != 3 {
		t.Errorf("NodeCount = %d, want 3", nc)
	}

	ec, _ := store.EdgeCount(ctx)
	if ec != 2 {
		t.Errorf("EdgeCount = %d, want 2", ec)
	}
}

func TestNodeCountByType(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store, []models.Node{
		makeNode("a", models.AssetVM, "tf"),
		makeNode("b", models.AssetVM, "tf"),
		makeNode("c", models.AssetDatabase, "tf"),
	}, nil)

	counts, _ := store.NodeCountByType(context.Background())
	if counts["vm"] != 2 {
		t.Errorf("vm count = %d, want 2", counts["vm"])
	}
	if counts["database"] != 1 {
		t.Errorf("database count = %d, want 1", counts["database"])
	}
}

func TestEdgeCountByType(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("a", models.AssetVM, "tf"),
			makeNode("b", models.AssetNetwork, "tf"),
			makeNode("c", models.AssetSubnet, "tf"),
		},
		[]models.Edge{
			makeEdge("a", "b", models.EdgeDependsOn),
			makeEdge("a", "c", models.EdgeConnectsTo),
			makeEdge("c", "b", models.EdgeDependsOn),
		},
	)

	counts, _ := store.EdgeCountByType(context.Background())
	if counts["depends_on"] != 2 {
		t.Errorf("depends_on = %d, want 2", counts["depends_on"])
	}
	if counts["connects_to"] != 1 {
		t.Errorf("connects_to = %d, want 1", counts["connects_to"])
	}
}

func TestExpiringNodes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	soon := time.Now().Add(5 * 24 * time.Hour).Truncate(time.Second)
	far := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second)

	n1 := makeNode("cert1", models.AssetCertificate, "tf")
	n1.ExpiresAt = &soon
	n2 := makeNode("cert2", models.AssetCertificate, "tf")
	n2.ExpiresAt = &far
	n3 := makeNode("vm1", models.AssetVM, "tf")
	// no expiry

	buildTestGraph(t, store, []models.Node{n1, n2, n3}, nil)

	expiring, _ := store.ExpiringNodes(ctx, 30)
	if len(expiring) != 1 {
		t.Errorf("expected 1 expiring node, got %d", len(expiring))
	}
	if len(expiring) > 0 && expiring[0].ID != "cert1" {
		t.Errorf("expected cert1, got %s", expiring[0].ID)
	}
}

func TestRecordAndListScans(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	id, err := store.RecordScan(ctx, Scan{
		Source: "terraform", SourcePath: "/path/to/state",
		StartedAt: time.Now(), Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Error("expected positive scan ID")
	}

	scans, _ := store.ListScans(ctx, 10)
	if len(scans) != 1 {
		t.Fatalf("expected 1 scan, got %d", len(scans))
	}
	if scans[0].Status != "running" {
		t.Errorf("status = %q, want %q", scans[0].Status, "running")
	}
}

func TestUpdateScan(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	id, _ := store.RecordScan(ctx, Scan{
		Source: "terraform", SourcePath: "/test",
		StartedAt: time.Now(), Status: "running",
	})

	_ = store.UpdateScan(ctx, id, "completed", 10, 5)

	scans, _ := store.ListScans(ctx, 10)
	if scans[0].Status != "completed" {
		t.Errorf("status = %q, want %q", scans[0].Status, "completed")
	}
	if scans[0].NodesFound != 10 {
		t.Errorf("NodesFound = %d, want 10", scans[0].NodesFound)
	}
	if scans[0].FinishedAt == nil {
		t.Error("FinishedAt should be set")
	}
}

func TestBuildAdjacency(t *testing.T) {
	store := newTestStore(t)
	buildTestGraph(t, store,
		[]models.Node{
			makeNode("a", models.AssetVM, "tf"),
			makeNode("b", models.AssetNetwork, "tf"),
			makeNode("c", models.AssetSubnet, "tf"),
		},
		[]models.Edge{
			makeEdge("a", "b", models.EdgeDependsOn),
			makeEdge("b", "c", models.EdgeDependsOn),
		},
	)

	down, up, err := store.BuildAdjacency(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(down["a"]) != 1 {
		t.Errorf("downstream[a] = %d, want 1", len(down["a"]))
	}
	if len(up["b"]) != 1 {
		t.Errorf("upstream[b] = %d, want 1", len(up["b"]))
	}
	if len(up["c"]) != 1 {
		t.Errorf("upstream[c] = %d, want 1", len(up["c"]))
	}
}

func TestGenerateEdgeID(t *testing.T) {
	id := GenerateEdgeID("a", "b", models.EdgeDependsOn)
	want := "a->depends_on->b"
	if id != want {
		t.Errorf("GenerateEdgeID = %q, want %q", id, want)
	}
}

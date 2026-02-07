package graph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
)

func TestNodeToParams(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	expires := now.Add(30 * 24 * time.Hour)
	n := models.Node{
		ID:         "vm:web1",
		Name:       "web1",
		Type:       models.AssetVM,
		Source:     "terraform",
		SourceFile: "main.tf",
		Provider:   "gcp",
		Metadata:   map[string]string{"region": "us-east1"},
		ExpiresAt:  &expires,
		LastSeen:   now,
		FirstSeen:  now,
	}

	params := nodeToParams(n)
	if params["id"] != "vm:web1" {
		t.Errorf("id = %v", params["id"])
	}
	if params["name"] != "web1" {
		t.Errorf("name = %v", params["name"])
	}
	if params["type"] != "vm" {
		t.Errorf("type = %v", params["type"])
	}
	if params["expiresAt"] != expires.Format(time.RFC3339) {
		t.Errorf("expiresAt = %v", params["expiresAt"])
	}
	// metadata should be JSON string
	metaStr, ok := params["metadata"].(string)
	if !ok || !strings.Contains(metaStr, "us-east1") {
		t.Errorf("metadata = %v", params["metadata"])
	}
}

func TestNodeToParams_NilExpiry(t *testing.T) {
	n := models.Node{
		ID: "a", Name: "a", Type: "vm", Source: "tf",
		Metadata: map[string]string{},
	}
	params := nodeToParams(n)
	if params["expiresAt"] != nil {
		t.Errorf("expiresAt should be nil, got %v", params["expiresAt"])
	}
}

func TestEdgeToParams(t *testing.T) {
	e := models.Edge{
		ID:       "a->b",
		FromID:   "a",
		ToID:     "b",
		Type:     models.EdgeDependsOn,
		Metadata: map[string]string{"via": "network"},
	}

	params := edgeToParams(e)
	if params["id"] != "a->b" {
		t.Errorf("id = %v", params["id"])
	}
	if params["fromID"] != "a" {
		t.Errorf("fromID = %v", params["fromID"])
	}
	if params["toID"] != "b" {
		t.Errorf("toID = %v", params["toID"])
	}
	if params["type"] != "depends_on" {
		t.Errorf("type = %v", params["type"])
	}
	metaStr, ok := params["metadata"].(string)
	if !ok || !strings.Contains(metaStr, "network") {
		t.Errorf("metadata = %v", params["metadata"])
	}
}

func TestSyncToMemgraph_EmptyGraph(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := &mockSession{}
	sf := mockSessionFactory(sess)
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))

	err := syncToMemgraph(ctx, store, sf, logger)
	if err != nil {
		t.Fatal(err)
	}

	// Should have: clear (1) + 3 indexes + no node/edge batches = 4 calls
	if len(sess.calls) != 4 {
		t.Errorf("expected 4 Run calls (clear + 3 indexes), got %d", len(sess.calls))
	}
}

func TestSyncToMemgraph_SmallBatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Insert 3 nodes and 2 edges
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

	sess := &mockSession{}
	sf := mockSessionFactory(sess)
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))

	err := syncToMemgraph(ctx, store, sf, logger)
	if err != nil {
		t.Fatal(err)
	}

	// 1 clear + 3 indexes + 1 node batch + 1 edge batch = 6
	if len(sess.calls) != 6 {
		t.Errorf("expected 6 Run calls, got %d", len(sess.calls))
	}
}

func TestSyncToMemgraph_LargeBatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Insert >500 nodes to trigger multiple batches
	for i := 0; i < 550; i++ {
		id := fmt.Sprintf("node-%d", i)
		_ = store.UpsertNode(ctx, makeNode(id, models.AssetVM, "tf"))
	}

	sess := &mockSession{}
	sf := mockSessionFactory(sess)
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))

	err := syncToMemgraph(ctx, store, sf, logger)
	if err != nil {
		t.Fatal(err)
	}

	// 1 clear + 3 indexes + 2 node batches (500 + 50) + 0 edge batches = 6
	if len(sess.calls) != 6 {
		t.Errorf("expected 6 Run calls (2 node batches), got %d", len(sess.calls))
	}
}

func TestSyncToMemgraph_ClearError(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sf := failSessionFactory(fmt.Errorf("clear failed"))
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))

	err := syncToMemgraph(ctx, store, sf, logger)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "clearing memgraph") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestSyncToMemgraph_NodeSyncError(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_ = store.UpsertNode(ctx, makeNode("A", models.AssetVM, "tf"))

	callCount := 0
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			callCount++
			// First 4 calls succeed (clear + 3 indexes), 5th (node batch) fails
			if callCount > 4 {
				return nil, fmt.Errorf("node sync error")
			}
			return &mockResult{}, nil
		},
	}
	sf := mockSessionFactory(sess)
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))

	err := syncToMemgraph(ctx, store, sf, logger)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "syncing node batch") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestSyncToMemgraph_EdgeSyncError(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	buildTestGraph(t, store,
		[]models.Node{
			makeNode("A", models.AssetVM, "tf"),
			makeNode("B", models.AssetNetwork, "tf"),
		},
		[]models.Edge{
			makeEdge("A", "B", models.EdgeDependsOn),
		},
	)

	callCount := 0
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			callCount++
			// First 5 calls succeed (clear + 3 indexes + 1 node batch), 6th (edge batch) fails
			if callCount > 5 {
				return nil, fmt.Errorf("edge sync error")
			}
			return &mockResult{}, nil
		},
	}
	sf := mockSessionFactory(sess)
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))

	err := syncToMemgraph(ctx, store, sf, logger)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "syncing edge batch") {
		t.Errorf("error = %q", err.Error())
	}
}

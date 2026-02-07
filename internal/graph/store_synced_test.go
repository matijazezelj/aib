package graph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/matijazezelj/aib/pkg/models"
)

func newTestSyncedStore(t *testing.T, sess *mockSession) (*SyncedStore, *SQLiteStore) {
	t.Helper()
	store := newTestStore(t)
	var sf sessionFactory
	if sess != nil {
		sf = mockSessionFactory(sess)
	}
	ss := &SyncedStore{
		SQLiteStore: store,
		newSession:  sf,
		logger:      slog.New(slog.NewTextHandler(nopWriter{}, nil)),
	}
	return ss, store
}

func TestSyncedStore_UpsertNode_NilSession(t *testing.T) {
	ss, store := newTestSyncedStore(t, nil)
	ctx := context.Background()

	node := makeNode("a", models.AssetVM, "tf")
	if err := ss.UpsertNode(ctx, node); err != nil {
		t.Fatal(err)
	}

	got, _ := store.GetNode(ctx, "a")
	if got == nil {
		t.Fatal("node should exist in SQLite")
	}
}

func TestSyncedStore_UpsertNode_WithMock(t *testing.T) {
	sess := &mockSession{}
	ss, _ := newTestSyncedStore(t, sess)
	ctx := context.Background()

	node := makeNode("a", models.AssetVM, "tf")
	if err := ss.UpsertNode(ctx, node); err != nil {
		t.Fatal(err)
	}

	if len(sess.calls) != 1 {
		t.Fatalf("expected 1 Run call, got %d", len(sess.calls))
	}
	if !strings.Contains(sess.calls[0].cypher, "MERGE") {
		t.Errorf("cypher should contain MERGE, got: %s", sess.calls[0].cypher)
	}
}

func TestSyncedStore_UpsertNode_SyncError(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return nil, fmt.Errorf("memgraph error")
		},
	}
	ss, store := newTestSyncedStore(t, sess)
	ctx := context.Background()

	node := makeNode("a", models.AssetVM, "tf")
	err := ss.UpsertNode(ctx, node)
	// Should not return error — sync errors are logged, not returned
	if err != nil {
		t.Fatalf("UpsertNode should succeed even if sync fails: %v", err)
	}

	// SQLite should still have the node
	got, _ := store.GetNode(ctx, "a")
	if got == nil {
		t.Fatal("node should exist in SQLite despite sync error")
	}
}

func TestSyncedStore_UpsertEdge_NilSession(t *testing.T) {
	ss, store := newTestSyncedStore(t, nil)
	ctx := context.Background()

	// Seed nodes first
	_ = store.UpsertNode(ctx, makeNode("a", models.AssetVM, "tf"))
	_ = store.UpsertNode(ctx, makeNode("b", models.AssetNetwork, "tf"))

	edge := makeEdge("a", "b", models.EdgeDependsOn)
	if err := ss.UpsertEdge(ctx, edge); err != nil {
		t.Fatal(err)
	}

	edges, _ := store.ListEdges(ctx, EdgeFilter{})
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
}

func TestSyncedStore_UpsertEdge_WithMock(t *testing.T) {
	sess := &mockSession{}
	ss, store := newTestSyncedStore(t, sess)
	ctx := context.Background()

	_ = store.UpsertNode(ctx, makeNode("a", models.AssetVM, "tf"))
	_ = store.UpsertNode(ctx, makeNode("b", models.AssetNetwork, "tf"))

	edge := makeEdge("a", "b", models.EdgeDependsOn)
	if err := ss.UpsertEdge(ctx, edge); err != nil {
		t.Fatal(err)
	}

	if len(sess.calls) != 1 {
		t.Fatalf("expected 1 Run call, got %d", len(sess.calls))
	}
	if !strings.Contains(sess.calls[0].cypher, "MERGE") {
		t.Errorf("cypher should contain MERGE, got: %s", sess.calls[0].cypher)
	}
}

func TestSyncedStore_UpsertEdge_SyncError(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return nil, fmt.Errorf("memgraph error")
		},
	}
	ss, store := newTestSyncedStore(t, sess)
	ctx := context.Background()

	_ = store.UpsertNode(ctx, makeNode("a", models.AssetVM, "tf"))
	_ = store.UpsertNode(ctx, makeNode("b", models.AssetNetwork, "tf"))

	edge := makeEdge("a", "b", models.EdgeDependsOn)
	err := ss.UpsertEdge(ctx, edge)
	if err != nil {
		t.Fatalf("UpsertEdge should succeed even if sync fails: %v", err)
	}
}

func TestSyncedStore_DeleteNode_NilSession(t *testing.T) {
	ss, store := newTestSyncedStore(t, nil)
	ctx := context.Background()

	_ = store.UpsertNode(ctx, makeNode("a", models.AssetVM, "tf"))
	if err := ss.DeleteNode(ctx, "a"); err != nil {
		t.Fatal(err)
	}

	got, _ := store.GetNode(ctx, "a")
	if got != nil {
		t.Error("node should be deleted")
	}
}

func TestSyncedStore_DeleteNode_WithMock(t *testing.T) {
	sess := &mockSession{}
	ss, store := newTestSyncedStore(t, sess)
	ctx := context.Background()

	_ = store.UpsertNode(ctx, makeNode("a", models.AssetVM, "tf"))
	if err := ss.DeleteNode(ctx, "a"); err != nil {
		t.Fatal(err)
	}

	if len(sess.calls) != 1 {
		t.Fatalf("expected 1 Run call, got %d", len(sess.calls))
	}
	if !strings.Contains(sess.calls[0].cypher, "DETACH DELETE") {
		t.Errorf("cypher should contain DETACH DELETE, got: %s", sess.calls[0].cypher)
	}
}

func TestSyncedStore_DeleteNode_SyncError(t *testing.T) {
	sess := &mockSession{
		runFunc: func(_ string, _ map[string]any) (resultIterator, error) {
			return nil, fmt.Errorf("memgraph error")
		},
	}
	ss, store := newTestSyncedStore(t, sess)
	ctx := context.Background()

	_ = store.UpsertNode(ctx, makeNode("a", models.AssetVM, "tf"))
	err := ss.DeleteNode(ctx, "a")
	if err != nil {
		t.Fatalf("DeleteNode should succeed even if sync fails: %v", err)
	}
}

func TestSyncedStore_Close_NilDriver(t *testing.T) {
	ss, _ := newTestSyncedStore(t, nil)
	err := ss.Close()
	if err != nil {
		t.Fatalf("Close with nil driver: %v", err)
	}
}

func TestSyncedStore_Close_WithDriver(t *testing.T) {
	store := newTestStore(t)
	driver := &mockDriver{}
	ss := &SyncedStore{
		SQLiteStore: store,
		driver:      driver,
		logger:      slog.New(slog.NewTextHandler(nopWriter{}, nil)),
	}

	_ = ss.Close()
	if !driver.closed {
		t.Error("driver should be closed")
	}
}

func TestSyncedStore_Close_DriverError(t *testing.T) {
	store := newTestStore(t)
	driver := &mockDriver{closeErr: fmt.Errorf("close error")}
	ss := &SyncedStore{
		SQLiteStore: store,
		driver:      driver,
		logger:      slog.New(slog.NewTextHandler(nopWriter{}, nil)),
	}

	err := ss.Close()
	if err == nil {
		t.Fatal("expected error from driver close")
	}
}

func TestSyncedStore_Underlying(t *testing.T) {
	ss, store := newTestSyncedStore(t, nil)
	if ss.Underlying() != store {
		t.Error("Underlying should return wrapped store")
	}
}

func TestSyncedStore_HasMemgraph(t *testing.T) {
	ss, _ := newTestSyncedStore(t, nil)
	if ss.HasMemgraph() {
		t.Error("HasMemgraph should be false with nil driver")
	}

	store := newTestStore(t)
	driver := &mockDriver{}
	ss2 := &SyncedStore{
		SQLiteStore: store,
		driver:      driver,
		logger:      slog.New(slog.NewTextHandler(nopWriter{}, nil)),
	}
	if !ss2.HasMemgraph() {
		t.Error("HasMemgraph should be true with non-nil driver")
	}
}

func TestSyncedStore_MemgraphDriver(t *testing.T) {
	ss, _ := newTestSyncedStore(t, nil)
	if ss.MemgraphDriver() != nil {
		t.Error("MemgraphDriver should be nil with nil driver")
	}

	store := newTestStore(t)
	driver := &mockDriver{}
	ss2 := &SyncedStore{
		SQLiteStore: store,
		driver:      driver,
		logger:      slog.New(slog.NewTextHandler(nopWriter{}, nil)),
	}
	if ss2.MemgraphDriver() != driver {
		t.Error("MemgraphDriver should return the driver")
	}
}

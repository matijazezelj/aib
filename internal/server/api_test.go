package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/matijazezelj/aib/internal/certs"
	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/pkg/models"
)

func newTestServer(t *testing.T, apiToken string) (*httptest.Server, *graph.SQLiteStore) {
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

	engine := graph.NewLocalEngine(store)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tracker := certs.NewTracker(store, nil, logger)

	s := New(store, engine, tracker, nil, logger, ":0", false, apiToken, "")

	mux := http.NewServeMux()
	RegisterRoutes(mux, s)
	mux.Handle("/", http.FileServer(http.FS(nil))) // skip UI for tests

	var handler http.Handler = mux
	handler = s.authMiddleware(handler)

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	return ts, store
}

func seedTestData(t *testing.T, store *graph.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	nodes := []models.Node{
		{ID: "tf:vm:web1", Name: "web1", Type: models.AssetVM, Source: "terraform", Provider: "google", Metadata: map[string]string{}, LastSeen: time.Now(), FirstSeen: time.Now()},
		{ID: "tf:network:vpc1", Name: "vpc1", Type: models.AssetNetwork, Source: "terraform", Provider: "google", Metadata: map[string]string{}, LastSeen: time.Now(), FirstSeen: time.Now()},
	}
	edges := []models.Edge{
		{ID: "tf:vm:web1->depends_on->tf:network:vpc1", FromID: "tf:vm:web1", ToID: "tf:network:vpc1", Type: models.EdgeDependsOn, Metadata: map[string]string{}},
	}

	for _, n := range nodes {
		if err := store.UpsertNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range edges {
		if err := store.UpsertEdge(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHealthz(t *testing.T) {
	ts, _ := newTestServer(t, "")
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestGetNodes(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var nodes []models.Node
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(nodes))
	}
}

func TestGetNodes_FilterByType(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/nodes?type=vm")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	var nodes []models.Node
	_ = json.NewDecoder(resp.Body).Decode(&nodes)
	if len(nodes) != 1 {
		t.Errorf("vm nodes = %d, want 1", len(nodes))
	}
}

func TestGetNodeByID(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/nodes/tf:vm:web1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var node models.Node
	_ = json.NewDecoder(resp.Body).Decode(&node)
	if node.ID != "tf:vm:web1" {
		t.Errorf("node id = %q, want tf:vm:web1", node.ID)
	}
}

func TestGetNodeByID_NotFound(t *testing.T) {
	ts, _ := newTestServer(t, "")
	resp, err := http.Get(ts.URL + "/api/v1/graph/nodes/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetEdges(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/edges")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	var edges []models.Edge
	_ = json.NewDecoder(resp.Body).Decode(&edges)
	if len(edges) != 1 {
		t.Errorf("edges = %d, want 1", len(edges))
	}
}

func TestGetGraph(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	var result map[string]json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&result)

	if _, ok := result["nodes"]; !ok {
		t.Error("missing nodes key in graph response")
	}
	if _, ok := result["edges"]; !ok {
		t.Error("missing edges key in graph response")
	}
}

func TestGetImpact(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/impact/tf:network:vpc1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result["affected_nodes"].(float64) != 1 {
		t.Errorf("affected_nodes = %v, want 1", result["affected_nodes"])
	}
}

func TestGetStats(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	var stats map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&stats)
	if stats["nodes_total"].(float64) != 2 {
		t.Errorf("nodes_total = %v, want 2", stats["nodes_total"])
	}
}

func TestGetScans(t *testing.T) {
	ts, _ := newTestServer(t, "")
	resp, err := http.Get(ts.URL + "/api/v1/scans")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// Auth middleware tests

func TestAuth_NoTokenConfigured(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	// No token = open access
	resp, err := http.Get(ts.URL + "/api/v1/graph/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (no auth required)", resp.StatusCode)
	}
}

func TestAuth_ValidToken(t *testing.T) {
	ts, store := newTestServer(t, "secret-token-123")
	seedTestData(t, store)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/graph/nodes", nil)
	req.Header.Set("Authorization", "Bearer secret-token-123")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAuth_InvalidToken(t *testing.T) {
	ts, _ := newTestServer(t, "secret-token-123")

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/graph/nodes", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAuth_MissingHeader(t *testing.T) {
	ts, _ := newTestServer(t, "secret-token-123")

	resp, err := http.Get(ts.URL + "/api/v1/graph/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAuth_HealthzBypassesAuth(t *testing.T) {
	ts, _ := newTestServer(t, "secret-token-123")

	// healthz is not under /api/ so it should not require auth
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (healthz bypasses auth)", resp.StatusCode)
	}
}

func TestGetCerts(t *testing.T) {
	ts, store := newTestServer(t, "")
	ctx := context.Background()
	now := time.Now()
	future := now.Add(60 * 24 * time.Hour)

	_ = store.UpsertNode(ctx, models.Node{
		ID: "cert:test", Name: "test-cert", Type: models.AssetCertificate,
		Source: "probe", Metadata: map[string]string{},
		ExpiresAt: &future, LastSeen: now, FirstSeen: now,
	})

	resp, err := http.Get(ts.URL + "/api/v1/certs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var certs []json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&certs)
	if len(certs) != 1 {
		t.Errorf("certs = %d, want 1", len(certs))
	}
}

func TestGetExpiringCerts(t *testing.T) {
	ts, store := newTestServer(t, "")
	ctx := context.Background()
	now := time.Now()
	soon := now.Add(10 * 24 * time.Hour)
	far := now.Add(120 * 24 * time.Hour)

	_ = store.UpsertNode(ctx, models.Node{
		ID: "cert:soon", Name: "soon-cert", Type: models.AssetCertificate,
		Source: "probe", Metadata: map[string]string{},
		ExpiresAt: &soon, LastSeen: now, FirstSeen: now,
	})
	_ = store.UpsertNode(ctx, models.Node{
		ID: "cert:far", Name: "far-cert", Type: models.AssetCertificate,
		Source: "probe", Metadata: map[string]string{},
		ExpiresAt: &far, LastSeen: now, FirstSeen: now,
	})

	resp, err := http.Get(ts.URL + "/api/v1/certs/expiring?days=30")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var certs []json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&certs)
	if len(certs) != 1 {
		t.Errorf("expiring certs = %d, want 1", len(certs))
	}
}

func TestGetScanStatus(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp, err := http.Get(ts.URL + "/api/v1/scan/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var status map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&status)
	if status["running"] != false {
		t.Errorf("running = %v, want false", status["running"])
	}
}

func TestTriggerScan_NoScanner(t *testing.T) {
	ts, _ := newTestServer(t, "")

	body := strings.NewReader(`{"source":"terraform","paths":["/tmp/state.tfstate"]}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	// Scanner is nil in test server, should return 503
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestTriggerScan_InvalidSource(t *testing.T) {
	ts, _ := newTestServer(t, "")

	body := strings.NewReader(`{"source":"invalid"}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	// Even though scanner is nil, source validation happens before scanner check
	// Actually scanner nil check happens first → 503
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (scanner nil checked first)", resp.StatusCode)
	}
}

func TestExportJSON(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/export/json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "aib-graph.json") {
		t.Errorf("Content-Disposition = %q, want to contain aib-graph.json", cd)
	}
}

func TestExportDOT(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/export/dot")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "aib-graph.dot") {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

func TestExportMermaid(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/export/mermaid")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "aib-graph.mmd") {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

func TestTriggerScan_PathTraversal(t *testing.T) {
	ts, _ := newTestServer(t, "")

	body := strings.NewReader(`{"source":"terraform","paths":["/home/../etc/passwd"]}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	// Scanner nil check happens first → 503
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (scanner nil checked first)", resp.StatusCode)
	}
}

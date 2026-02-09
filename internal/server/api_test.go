package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/matijazezelj/aib/internal/certs"
	"github.com/matijazezelj/aib/internal/config"
	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/internal/scanner"
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

	s := New(store, engine, tracker, nil, logger, ":0", false, apiToken, "", nil, "test")

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

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
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

func seedChainData(t *testing.T, store *graph.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	nodes := []models.Node{
		{ID: "tf:lb:frontend", Name: "frontend-lb", Type: models.AssetLoadBalancer, Source: "terraform", Provider: "google", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now},
		{ID: "tf:vm:app", Name: "app-server", Type: models.AssetVM, Source: "terraform", Provider: "google", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now},
		{ID: "tf:db:primary", Name: "primary-db", Type: models.AssetDatabase, Source: "terraform", Provider: "google", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now},
	}
	edges := []models.Edge{
		{ID: "e1", FromID: "tf:lb:frontend", ToID: "tf:vm:app", Type: models.EdgeDependsOn, Metadata: map[string]string{}},
		{ID: "e2", FromID: "tf:vm:app", ToID: "tf:db:primary", Type: models.EdgeDependsOn, Metadata: map[string]string{}},
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

func TestShortestPath(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedChainData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/shortest-path?from=tf:lb:frontend&to=tf:db:primary")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&result)

	var nodes []models.Node
	_ = json.Unmarshal(result["nodes"], &nodes)
	if len(nodes) != 3 {
		t.Errorf("path nodes = %d, want 3", len(nodes))
	}
}

func TestShortestPath_MissingParams(t *testing.T) {
	ts, _ := newTestServer(t, "")

	tests := []struct {
		name string
		url  string
	}{
		{"no params", "/api/v1/graph/shortest-path"},
		{"only from", "/api/v1/graph/shortest-path?from=a"},
		{"only to", "/api/v1/graph/shortest-path?to=b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tt.url)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close() //nolint:errcheck // test cleanup

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestDependencyChain(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedChainData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/dependency-chain/tf:lb:frontend")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Nodes []models.Node `json:"nodes"`
		Depth int           `json:"depth"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Nodes) != 2 {
		t.Errorf("deps = %d, want 2 (app + db)", len(result.Nodes))
	}
	if result.Depth != 10 {
		t.Errorf("depth = %d, want 10 (default)", result.Depth)
	}
}

func TestDependencyChain_DepthParam(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedChainData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/dependency-chain/tf:lb:frontend?depth=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Nodes []models.Node `json:"nodes"`
		Depth int           `json:"depth"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Nodes) != 1 {
		t.Errorf("deps = %d, want 1 (only app at depth 1)", len(result.Nodes))
	}
	if result.Depth != 1 {
		t.Errorf("depth = %d, want 1", result.Depth)
	}
}

func TestMetrics(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	for _, metric := range []string{
		"aib_nodes_total 2",
		"aib_edges_total 1",
		"aib_certs_expiring_total",
		"aib_scans_completed_total",
		"aib_scans_failed_total",
		"aib_build_info{version=\"test\"} 1",
	} {
		if !strings.Contains(text, metric) {
			t.Errorf("metrics missing %q", metric)
		}
	}
}

func TestMetrics_NoAuth(t *testing.T) {
	ts, _ := newTestServer(t, "secret-token-123")

	// /metrics is not under /api/ so it should bypass auth
	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (metrics bypasses auth)", resp.StatusCode)
	}
}

func TestTriggerScan_PathTraversal(t *testing.T) {
	ts, _ := newTestServer(t, "")

	// Path with .. that stays absolute after Clean (e.g., /home/../etc/passwd → /etc/passwd)
	// passes validation since the cleaned result has no traversal.
	// Use a relative traversal path to test actual rejection.
	body := strings.NewReader(`{"source":"terraform","paths":["../etc/passwd"]}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (path traversal rejected)", resp.StatusCode)
	}
}

// --- Graph analysis endpoint tests ---

func TestHandleCycles(t *testing.T) {
	ts, store := newTestServer(t, "")
	ctx := context.Background()
	now := time.Now()
	_ = store.UpsertNode(ctx, models.Node{ID: "A", Name: "A", Type: models.AssetVM, Source: "tf", Provider: "test", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertNode(ctx, models.Node{ID: "B", Name: "B", Type: models.AssetNetwork, Source: "tf", Provider: "test", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertEdge(ctx, models.Edge{ID: "A->B", FromID: "A", ToID: "B", Type: models.EdgeDependsOn, Metadata: map[string]string{}})
	_ = store.UpsertEdge(ctx, models.Edge{ID: "B->A", FromID: "B", ToID: "A", Type: models.EdgeDependsOn, Metadata: map[string]string{}})

	resp, err := http.Get(ts.URL + "/api/v1/graph/analysis/cycles")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	count := int(result["count"].(float64))
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestHandleSPOF(t *testing.T) {
	ts, store := newTestServer(t, "")
	ctx := context.Background()
	now := time.Now()
	_ = store.UpsertNode(ctx, models.Node{ID: "A", Name: "A", Type: models.AssetVM, Source: "tf", Provider: "test", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertNode(ctx, models.Node{ID: "B", Name: "B", Type: models.AssetNetwork, Source: "tf", Provider: "test", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertNode(ctx, models.Node{ID: "C", Name: "C", Type: models.AssetSubnet, Source: "tf", Provider: "test", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertEdge(ctx, models.Edge{ID: "A->C", FromID: "A", ToID: "C", Type: models.EdgeDependsOn, Metadata: map[string]string{}})
	_ = store.UpsertEdge(ctx, models.Edge{ID: "B->C", FromID: "B", ToID: "C", Type: models.EdgeDependsOn, Metadata: map[string]string{}})

	resp, err := http.Get(ts.URL + "/api/v1/graph/analysis/spof?min_affected=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	count := int(result["count"].(float64))
	if count == 0 {
		t.Error("expected at least one SPOF")
	}
}

func TestHandleOrphans(t *testing.T) {
	ts, store := newTestServer(t, "")
	ctx := context.Background()
	now := time.Now()
	_ = store.UpsertNode(ctx, models.Node{ID: "A", Name: "A", Type: models.AssetVM, Source: "tf", Provider: "test", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertNode(ctx, models.Node{ID: "B", Name: "B", Type: models.AssetNetwork, Source: "tf", Provider: "test", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertNode(ctx, models.Node{ID: "C", Name: "C", Type: models.AssetSubnet, Source: "tf", Provider: "test", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertEdge(ctx, models.Edge{ID: "A->B", FromID: "A", ToID: "B", Type: models.EdgeDependsOn, Metadata: map[string]string{}})

	resp, err := http.Get(ts.URL + "/api/v1/graph/analysis/orphans")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	count := int(result["count"].(float64))
	if count != 1 {
		t.Errorf("orphan count = %d, want 1", count)
	}
}

func TestHandlePlanImpact(t *testing.T) {
	ts, store := newTestServer(t, "")
	ctx := context.Background()
	now := time.Now()

	// Create a plan node with update action
	_ = store.UpsertNode(ctx, models.Node{
		ID: "tf:database:prod-db", Name: "prod-db", Type: models.AssetDatabase,
		Source: "terraform-plan", Provider: "aws",
		Metadata: map[string]string{"plan_action": "update"},
		LastSeen: now, FirstSeen: now,
	})

	// Create a dependent node
	_ = store.UpsertNode(ctx, models.Node{
		ID: "tf:vm:web", Name: "web", Type: models.AssetVM,
		Source: "terraform", Provider: "aws",
		Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	})
	_ = store.UpsertEdge(ctx, models.Edge{
		ID: "web->db", FromID: "tf:vm:web", ToID: "tf:database:prod-db",
		Type: models.EdgeDependsOn, Metadata: map[string]string{},
	})

	resp, err := http.Get(ts.URL + "/api/v1/plan/impact")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	count := int(result["count"].(float64))
	if count != 1 {
		t.Errorf("plan nodes = %d, want 1", count)
	}

	planNodes := result["plan_nodes"].([]any)
	pn := planNodes[0].(map[string]any)
	if pn["action"] != "update" {
		t.Errorf("action = %v, want update", pn["action"])
	}
	if int(pn["affected_count"].(float64)) != 1 {
		t.Errorf("affected_count = %v, want 1", pn["affected_count"])
	}
}

func newTestServerWithAllowedPaths(t *testing.T, allowedPaths []string) *httptest.Server {
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

	s := New(store, engine, tracker, nil, logger, ":0", false, "", "", allowedPaths, "test")

	mux := http.NewServeMux()
	RegisterRoutes(mux, s)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return ts
}

func TestTriggerScan_AllowedPaths_Blocked(t *testing.T) {
	ts := newTestServerWithAllowedPaths(t, []string{"/opt/infra/terraform"})

	body := strings.NewReader(`{"source":"terraform","paths":["/etc/secrets"]}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestHandleScanDiff(t *testing.T) {
	ts, store := newTestServer(t, "")
	ctx := context.Background()

	// Record a scan and store a drift summary
	scanID, err := store.RecordScan(ctx, graph.Scan{
		Source:     "terraform",
		SourcePath: "/tmp/test.tfstate",
		StartedAt:  time.Now(),
		Status:     "completed",
	})
	if err != nil {
		t.Fatal(err)
	}

	diff := &graph.DriftSummary{
		NodesAdded: []graph.NodeChange{
			{ID: "tf:vm:new", Name: "new", Type: "vm"},
		},
		NodesRemoved: []graph.NodeChange{
			{ID: "tf:db:old", Name: "old", Type: "database"},
		},
	}
	if err := store.StoreDiff(ctx, scanID, diff); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + fmt.Sprintf("/api/v1/scans/%d/diff", scanID))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
	}

	var got graph.DriftSummary
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.NodesAdded) != 1 {
		t.Errorf("nodes_added = %d, want 1", len(got.NodesAdded))
	}
	if len(got.NodesRemoved) != 1 {
		t.Errorf("nodes_removed = %d, want 1", len(got.NodesRemoved))
	}
}

func TestHandleScanDiff_NotFound(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp, err := http.Get(ts.URL + "/api/v1/scans/99999/diff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTriggerScan_AllowedPaths_Permitted(t *testing.T) {
	ts := newTestServerWithAllowedPaths(t, []string{"/opt/infra/terraform"})

	body := strings.NewReader(`{"source":"terraform","paths":["/opt/infra/terraform/main.tfstate"]}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	// Scanner is nil, so path check passes but scanner returns 503
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (scanner nil, but path check passed)", resp.StatusCode)
	}
}

func TestTriggerScan_AllowedPaths_Empty(t *testing.T) {
	ts := newTestServerWithAllowedPaths(t, nil)

	body := strings.NewReader(`{"source":"terraform","paths":["/any/path/state.tfstate"]}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	// No allowlist = all paths allowed; scanner nil → 503
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (no allowlist, scanner nil)", resp.StatusCode)
	}
}

func newTestServerWithScanner(t *testing.T) (*httptest.Server, *graph.SQLiteStore) {
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

	cfg := &config.Config{}
	sc := scanner.New(store, cfg, logger)

	s := New(store, engine, tracker, sc, logger, ":0", false, "", "", nil, "test")

	mux := http.NewServeMux()
	RegisterRoutes(mux, s)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return ts, store
}

func TestHandleOpenAPISpec(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp, err := http.Get(ts.URL + "/api/v1/openapi.json")
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
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Error("expected non-empty OpenAPI spec")
	}
}

func TestHandleAPIDocs(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp, err := http.Get(ts.URL + "/api/docs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestHandleGraph_WithData(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Nodes []json.RawMessage `json:"nodes"`
		Edges []json.RawMessage `json:"edges"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(result.Nodes))
	}
	if len(result.Edges) != 1 {
		t.Errorf("edges = %d, want 1", len(result.Edges))
	}
}

func TestTriggerScan_MissingPaths(t *testing.T) {
	ts, _ := newTestServerWithScanner(t)

	body := strings.NewReader(`{"source":"terraform"}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTriggerScan_KubernetesLive_NoPaths(t *testing.T) {
	ts, _ := newTestServerWithScanner(t)

	body := strings.NewReader(`{"source":"kubernetes-live"}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	// kubernetes-live doesn't require paths — should get 202 (scan started, even if it fails internally)
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
}

func TestTriggerScan_ValidSource_WithScanner(t *testing.T) {
	ts, store := newTestServerWithScanner(t)

	// Seed a tfstate file for the scanner to find
	testdata := t.TempDir()
	// Write a minimal .tfstate so the scanner path validation passes
	tfstate := `{"version":4,"resources":[{"mode":"managed","type":"aws_instance","name":"test","provider":"provider[\"registry.terraform.io/hashicorp/aws\"]","instances":[{"attributes":{"name":"test-vm"},"dependencies":[]}]}]}`
	if err := os.WriteFile(testdata+"/test.tfstate", []byte(tfstate), 0o644); err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(fmt.Sprintf(`{"source":"terraform","paths":[%q]}`, testdata+"/test.tfstate"))
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 202, body: %s", resp.StatusCode, bodyBytes)
	}

	// Wait a bit for async scan to finish
	time.Sleep(500 * time.Millisecond)

	// Verify nodes were stored
	nodes, _ := store.ListNodes(context.Background(), graph.NodeFilter{})
	if len(nodes) == 0 {
		t.Error("expected nodes after scan")
	}
}

func TestTriggerScan_AllSource(t *testing.T) {
	ts, _ := newTestServerWithScanner(t)

	body := strings.NewReader(`{"source":"all"}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
}

func TestCORSMiddleware(t *testing.T) {
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

	s := New(store, engine, tracker, nil, logger, ":0", false, "", "https://example.com", nil, "test")

	mux := http.NewServeMux()
	RegisterRoutes(mux, s)

	var handler http.Handler = mux
	handler = s.corsMiddleware(handler)

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	// Regular GET should have CORS headers
	resp, err := http.Get(ts.URL + "/api/v1/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("CORS Allow-Origin = %q, want https://example.com", got)
	}

	// OPTIONS preflight
	req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/v1/stats", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close() //nolint:errcheck // test cleanup

	if resp2.StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", resp2.StatusCode)
	}
}

func TestHandleGraph_Empty(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp, err := http.Get(ts.URL + "/api/v1/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestGetNodes_FilterBySource(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/nodes?source=terraform")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	var nodes []models.Node
	_ = json.NewDecoder(resp.Body).Decode(&nodes)
	if len(nodes) != 2 {
		t.Errorf("terraform nodes = %d, want 2", len(nodes))
	}
}

func TestGetNodes_FilterByProvider(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/nodes?provider=google")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	var nodes []models.Node
	_ = json.NewDecoder(resp.Body).Decode(&nodes)
	if len(nodes) != 2 {
		t.Errorf("google nodes = %d, want 2", len(nodes))
	}
}

func TestGetEdges_FilterByType(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/edges?type=depends_on")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	var edges []models.Edge
	_ = json.NewDecoder(resp.Body).Decode(&edges)
	if len(edges) != 1 {
		t.Errorf("depends_on edges = %d, want 1", len(edges))
	}
}

func TestGetEdges_FilterByFrom(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/edges?from=tf:vm:web1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	var edges []models.Edge
	_ = json.NewDecoder(resp.Body).Decode(&edges)
	if len(edges) != 1 {
		t.Errorf("edges from web1 = %d, want 1", len(edges))
	}
}

func TestGetEdges_FilterByTo(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/edges?to=tf:network:vpc1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	var edges []models.Edge
	_ = json.NewDecoder(resp.Body).Decode(&edges)
	if len(edges) != 1 {
		t.Errorf("edges to vpc1 = %d, want 1", len(edges))
	}
}

func TestGetExpiringCerts_WithDaysParam(t *testing.T) {
	ts, store := newTestServer(t, "")
	ctx := context.Background()
	now := time.Now()
	expires := now.Add(15 * 24 * time.Hour)
	_ = store.UpsertNode(ctx, models.Node{
		ID: "cert:test", Name: "test-cert", Type: models.AssetCertificate,
		Source: "terraform", Provider: "tls", ExpiresAt: &expires,
		Metadata: map[string]string{}, LastSeen: now, FirstSeen: now,
	})

	resp, err := http.Get(ts.URL + "/api/v1/certs/expiring?days=20")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var certs []any
	_ = json.NewDecoder(resp.Body).Decode(&certs)
	if len(certs) != 1 {
		t.Errorf("expiring certs = %d, want 1", len(certs))
	}
}

func TestGetSPOF_WithLimit(t *testing.T) {
	ts, store := newTestServer(t, "")
	ctx := context.Background()
	now := time.Now()
	// Create a hub node that many depend on
	_ = store.UpsertNode(ctx, models.Node{ID: "hub", Name: "hub", Type: models.AssetNetwork, Source: "tf", Provider: "test", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("spoke-%d", i)
		_ = store.UpsertNode(ctx, models.Node{ID: id, Name: id, Type: models.AssetVM, Source: "tf", Provider: "test", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
		_ = store.UpsertEdge(ctx, models.Edge{ID: id + "->hub", FromID: id, ToID: "hub", Type: models.EdgeDependsOn, Metadata: map[string]string{}})
	}

	resp, err := http.Get(ts.URL + "/api/v1/graph/analysis/spof?limit=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	count := int(result["count"].(float64))
	if count > 1 {
		t.Errorf("count = %d, want <= 1 (limit=1)", count)
	}
}

func TestDependencyChain_CustomDepth(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/dependency-chain/tf:vm:web1?depth=5")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	depth := int(result["depth"].(float64))
	if depth != 5 {
		t.Errorf("depth = %d, want 5", depth)
	}
}

func TestDependencyChain_InvalidDepth(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/dependency-chain/tf:vm:web1?depth=abc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	depth := int(result["depth"].(float64))
	if depth != 10 {
		t.Errorf("depth = %d, want 10 (default)", depth)
	}
}

func TestHandleScanDiff_InvalidID(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp, err := http.Get(ts.URL + "/api/v1/scans/abc/diff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestExportJSON_WithData(t *testing.T) {
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
		t.Errorf("content-type = %q, want application/json", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) < 10 {
		t.Error("expected non-trivial JSON export")
	}
}

func TestExportDOT_WithData(t *testing.T) {
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
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "digraph") {
		t.Error("expected DOT format with 'digraph'")
	}
}

func TestExportMermaid_WithData(t *testing.T) {
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
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "graph") {
		t.Error("expected Mermaid format with 'graph'")
	}
}

func TestHandleScans_Empty(t *testing.T) {
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

func TestHandleCerts_WithData(t *testing.T) {
	ts, store := newTestServer(t, "")
	ctx := context.Background()
	now := time.Now()
	expires := now.Add(30 * 24 * time.Hour)
	_ = store.UpsertNode(ctx, models.Node{
		ID: "cert:test", Name: "test-cert", Type: models.AssetCertificate,
		Source: "terraform", Provider: "tls", ExpiresAt: &expires,
		Metadata: map[string]string{}, LastSeen: now, FirstSeen: now,
	})

	resp, err := http.Get(ts.URL + "/api/v1/certs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var certs []any
	_ = json.NewDecoder(resp.Body).Decode(&certs)
	if len(certs) != 1 {
		t.Errorf("certs = %d, want 1", len(certs))
	}
}

func TestHandleScanStatus_WithScanner(t *testing.T) {
	ts, _ := newTestServerWithScanner(t)

	resp, err := http.Get(ts.URL + "/api/v1/scan/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if _, ok := result["running"]; !ok {
		t.Error("expected 'running' field in response")
	}
}

func TestMetrics_WithScans(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	// Record a scan so the metrics loop has scan data to iterate
	scanID, _ := store.RecordScan(context.Background(), graph.Scan{
		Source:     "terraform",
		SourcePath: "/tmp/test",
		Status:     "completed",
		StartedAt:  time.Now().Add(-time.Minute),
	})
	_ = store.UpdateScan(context.Background(), scanID, "completed", 2, 1)

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "aib_scans_completed_total 1") {
		t.Errorf("expected completed scan count of 1 in metrics, got: %s", s)
	}
}

func TestTriggerScan_ReadOnly(t *testing.T) {
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

	// readOnly = true
	s := New(store, engine, tracker, nil, logger, ":0", true, "", "", nil, "test")

	mux := http.NewServeMux()
	RegisterRoutes(mux, s)

	tserver := httptest.NewServer(mux)
	t.Cleanup(tserver.Close)

	body := strings.NewReader(`{"source":"terraform","paths":["/tmp/test"]}`)
	resp, err := http.Post(tserver.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	// In read-only mode, the POST route is not registered
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusOK {
		t.Errorf("status = %d, expected non-success in read-only mode", resp.StatusCode)
	}
}

func TestTriggerScan_InvalidJSON(t *testing.T) {
	ts, _ := newTestServerWithScanner(t)

	body := strings.NewReader(`not json`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTriggerScan_InvalidNamespace(t *testing.T) {
	ts, _ := newTestServerWithScanner(t)

	body := strings.NewReader(`{"source":"kubernetes-live","namespaces":["INVALID!@#"]}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTriggerScan_InvalidPlaybooks(t *testing.T) {
	ts, _ := newTestServerWithScanner(t)

	body := strings.NewReader(`{"source":"ansible","paths":["/tmp/valid"],"playbooks":"relative/path"}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTriggerScan_AllSource_NoScanner(t *testing.T) {
	ts, _ := newTestServer(t, "")

	body := strings.NewReader(`{"source":"all"}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (scanner not configured)", resp.StatusCode)
	}
}

func TestTriggerScan_InvalidValuesFile(t *testing.T) {
	ts, _ := newTestServerWithScanner(t)

	body := strings.NewReader(`{"source":"kubernetes","paths":["/tmp/manifests"],"helm":true,"values_file":"relative/values.yaml"}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (relative values_file)", resp.StatusCode)
	}
}

func TestTriggerScan_PathTraversalInPaths(t *testing.T) {
	ts, _ := newTestServerWithScanner(t)

	body := strings.NewReader(`{"source":"terraform","paths":["relative/path"]}`)
	resp, err := http.Post(ts.URL+"/api/v1/scan", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (relative path)", resp.StatusCode)
	}
}

func TestGetNodes_FilterBySourceNoResults(t *testing.T) {
	ts, store := newTestServer(t, "")
	seedTestData(t, store)

	resp, err := http.Get(ts.URL + "/api/v1/graph/nodes?source=nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRateLimiter(t *testing.T) {
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

	s := New(store, engine, tracker, nil, logger, ":0", false, "", "", nil, "test")
	s.done = make(chan struct{})

	mux := http.NewServeMux()
	RegisterRoutes(mux, s)

	var handler http.Handler = mux
	handler = s.rateLimiter(handler)

	ts := httptest.NewServer(handler)
	t.Cleanup(func() {
		close(s.done)
		ts.Close()
	})

	// Hit the rate limiter hard — burst is 20, rate is 10/sec
	got429 := false
	for i := 0; i < 30; i++ {
		resp, err := http.Get(ts.URL + "/api/v1/stats")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close() //nolint:errcheck // test cleanup
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("expected 429 after exceeding rate limit")
	}
}

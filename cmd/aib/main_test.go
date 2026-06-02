package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/internal/scanner"
	"github.com/matijazezelj/aib/pkg/models"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

// newTestApp returns a cliApp wired to a temp DB and captured output buffer.
func newTestApp(t *testing.T) (*cliApp, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	app := &cliApp{
		dbPath:       filepath.Join(t.TempDir(), "test.db"),
		logFormat:    "text",
		logLevel:     "info",
		outputFormat: "text",
		version:      "test",
		out:          &buf,
		errOut:       io.Discard,
		in:           strings.NewReader(""),
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return app, &buf
}

// seedTestData pre-populates 2 nodes and 1 edge via the app's store, then closes it.
// The caller's command will reopen the store via a.openStore().
func seedTestData(t *testing.T, app *cliApp) {
	t.Helper()
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	if err := store.UpsertNode(ctx, models.Node{
		ID: "vm:web1", Name: "web1", Type: models.AssetVM,
		Source: "terraform", Provider: "aws", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertNode(ctx, models.Node{
		ID: "db:pg1", Name: "pg1", Type: models.AssetDatabase,
		Source: "terraform", Provider: "aws", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertEdge(ctx, models.Edge{
		FromID: "vm:web1", ToID: "db:pg1", Type: models.EdgeDependsOn,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

// runCmd executes a cobra command attached to a root, returning any error.
func runCmd(app *cliApp, cmd *cobra.Command, args ...string) error {
	return runCmdWithContext(context.Background(), app, cmd, args...)
}

func runCmdWithContext(ctx context.Context, app *cliApp, cmd *cobra.Command, args ...string) error {
	root := &cobra.Command{Use: "aib"}
	root.AddCommand(cmd)
	root.SetArgs(args)
	root.SetOut(app.out)
	root.SetErr(app.errOut)
	root.SetContext(ctx)
	return root.Execute()
}

// --- Pure utility tests (no cliApp needed) ---

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"DEBUG", slog.LevelDebug, false},
		{"INFO", slog.LevelInfo, false},
		{"WARN", slog.LevelWarn, false},
		{"Error", slog.LevelError, false},
		{"invalid", slog.LevelInfo, true},
		{"", slog.LevelInfo, true},
		{"trace", slog.LevelInfo, true},
	}

	for _, tt := range tests {
		got, err := parseLogLevel(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseLogLevel(%q) expected error", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseLogLevel(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCountTreeNodes(t *testing.T) {
	tree := &graph.ImpactNode{
		NodeID: "root",
		Children: []graph.ImpactNode{
			{NodeID: "child1"},
			{NodeID: "child2", Children: []graph.ImpactNode{
				{NodeID: "grandchild"},
			}},
		},
	}

	count := countTreeNodes(tree)
	if count != 4 {
		t.Errorf("countTreeNodes = %d, want 4", count)
	}
}

func TestCountTreeNodes_Leaf(t *testing.T) {
	tree := &graph.ImpactNode{NodeID: "leaf"}
	if count := countTreeNodes(tree); count != 1 {
		t.Errorf("countTreeNodes(leaf) = %d, want 1", count)
	}
}

func TestCollectWarnings_NoExpiry(t *testing.T) {
	tree := &graph.ImpactNode{
		NodeID: "root",
		Node:   &models.Node{ID: "root"},
	}
	warnings := collectWarnings(tree)
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d", len(warnings))
	}
}

func TestCollectWarnings_ExpiringCert(t *testing.T) {
	soon := time.Now().Add(5 * 24 * time.Hour)
	tree := &graph.ImpactNode{
		NodeID: "cert1",
		Node: &models.Node{
			ID:        "cert1",
			ExpiresAt: &soon,
		},
	}
	warnings := collectWarnings(tree)
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warnings))
	}
}

func TestCollectWarnings_Recursive(t *testing.T) {
	soon := time.Now().Add(5 * 24 * time.Hour)
	far := time.Now().Add(90 * 24 * time.Hour)
	tree := &graph.ImpactNode{
		NodeID: "root",
		Node:   &models.Node{ID: "root", ExpiresAt: &soon},
		Children: []graph.ImpactNode{
			{
				NodeID: "child",
				Node:   &models.Node{ID: "child", ExpiresAt: &far},
			},
			{
				NodeID: "child2",
				Node:   &models.Node{ID: "child2", ExpiresAt: &soon},
			},
		},
	}
	warnings := collectWarnings(tree)
	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d", len(warnings))
	}
}

// --- version ---

func TestVersionCmd(t *testing.T) {
	app, buf := newTestApp(t)
	cmd := app.versionCmd()
	cmd.Run(cmd, nil)

	output := buf.String()
	if !strings.Contains(output, "aib") {
		t.Errorf("version output should contain 'aib', got %q", output)
	}
	if !strings.Contains(output, "test") {
		t.Errorf("version output should contain 'test', got %q", output)
	}
}

// --- completion ---

func TestCompletionCmd_Bash(t *testing.T) {
	app, buf := newTestApp(t)
	err := runCmd(app, app.completionCmd(), "completion", "bash")
	if err != nil {
		t.Fatalf("completion bash error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("completion bash produced no output")
	}
}

func TestCompletionCmd_Zsh(t *testing.T) {
	app, buf := newTestApp(t)
	err := runCmd(app, app.completionCmd(), "completion", "zsh")
	if err != nil {
		t.Fatalf("completion zsh error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("completion zsh produced no output")
	}
}

func TestCompletionCmd_Fish(t *testing.T) {
	app, buf := newTestApp(t)
	err := runCmd(app, app.completionCmd(), "completion", "fish")
	if err != nil {
		t.Fatalf("completion fish error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("completion fish produced no output")
	}
}

func TestCompletionCmd_PowerShell(t *testing.T) {
	app, buf := newTestApp(t)
	err := runCmd(app, app.completionCmd(), "completion", "powershell")
	if err != nil {
		t.Fatalf("completion powershell error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("completion powershell produced no output")
	}
}

func TestCompletionCmd_InvalidShell(t *testing.T) {
	app, _ := newTestApp(t)
	err := runCmd(app, app.completionCmd(), "completion", "invalid")
	if err == nil {
		t.Error("expected error for invalid shell")
	}
}

// --- printScanResult ---

func TestPrintScanResult_Success(t *testing.T) {
	app, buf := newTestApp(t)
	app.printScanResult(scanner.ScanResult{
		ScanID:     1,
		NodesFound: 10,
		EdgesFound: 5,
		Warnings:   []string{"missing provider"},
	})

	output := buf.String()
	if !strings.Contains(output, "10 nodes") {
		t.Errorf("output should mention nodes, got: %s", output)
	}
	if !strings.Contains(output, "5 edges") {
		t.Errorf("output should mention edges, got: %s", output)
	}
	if !strings.Contains(output, "missing provider") {
		t.Errorf("output should mention warning, got: %s", output)
	}
}

func TestPrintScanResult_Error(t *testing.T) {
	app, buf := newTestApp(t)
	app.printScanResult(scanner.ScanResult{
		Error: fmt.Errorf("scan failed"),
	})

	output := buf.String()
	if !strings.Contains(output, "failed") {
		t.Errorf("output should mention failure, got: %s", output)
	}
}

// --- printTree ---

func TestPrintTree(t *testing.T) {
	app, buf := newTestApp(t)

	tree := &graph.ImpactNode{
		NodeID: "root",
		Node:   &models.Node{ID: "root", Type: models.AssetVM},
		Children: []graph.ImpactNode{
			{
				NodeID:   "child1",
				Node:     &models.Node{ID: "child1", Type: models.AssetNetwork},
				EdgeType: models.EdgeDependsOn,
			},
			{
				NodeID:   "child2",
				Node:     &models.Node{ID: "child2", Type: models.AssetDatabase},
				EdgeType: models.EdgeConnectsTo,
			},
		},
	}

	app.printTree(context.Background(), tree, "  ", true)

	output := buf.String()
	if !strings.Contains(output, "root") {
		t.Errorf("output should contain root, got: %s", output)
	}
	if !strings.Contains(output, "child1") {
		t.Errorf("output should contain child1, got: %s", output)
	}
}

// --- openStore error handling ---

func TestOpenStore_InvalidConfig(t *testing.T) {
	app, _ := newTestApp(t)
	app.cfgFile = "/nonexistent/config.yaml"
	_, _, err := app.openStore()
	if err == nil {
		t.Error("expected error for nonexistent config")
	}
}

// --- graph show ---

func TestGraphShowCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphShowCmd(), "show")
	if err != nil {
		t.Fatalf("graph show error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Total nodes: 2") {
		t.Errorf("expected 'Total nodes: 2' in output, got: %s", output)
	}
	if !strings.Contains(output, "Total edges: 1") {
		t.Errorf("expected 'Total edges: 1' in output, got: %s", output)
	}
}

func TestReportCmd_Markdown(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.reportCmd(), "report", "--format", "markdown")
	if err != nil {
		t.Fatalf("report markdown error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"# AIB Infrastructure Report",
		"Total nodes: 2",
		"Total edges: 1",
		"vm:web1",
		"db:pg1",
		"Security findings: 1",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in markdown report, got: %s", want, output)
		}
	}
}

func TestReportCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.reportCmd(), "report", "--format", "json")
	if err != nil {
		t.Fatalf("report json error: %v", err)
	}

	var report struct {
		TotalNodes int `json:"total_nodes"`
		TotalEdges int `json:"total_edges"`
		Audit      struct {
			Summary graph.AuditSummary `json:"summary"`
		} `json:"audit"`
	}
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("report JSON is invalid: %v\nOutput: %s", err, buf.String())
	}
	if report.TotalNodes != 2 || report.TotalEdges != 1 {
		t.Fatalf("unexpected graph counts: nodes=%d edges=%d", report.TotalNodes, report.TotalEdges)
	}
	if report.Audit.Summary.Total != 1 {
		t.Fatalf("expected one info finding for missing encryption config, got %+v", report.Audit.Summary)
	}
}

func TestDetectAutoScanRequests(t *testing.T) {
	reqs := detectAutoScanRequests([]string{
		"infra/terraform.tfstate",
		"plan/tfplan.json",
		"deploy/docker-compose.yml",
		"k8s/deployment.yaml",
		"cloudformation/template.yaml",
		"pulumi/stack.json",
	})

	want := map[string][]string{
		"terraform":      {"infra/terraform.tfstate"},
		"terraform-plan": {"plan/tfplan.json"},
		"compose":        {"deploy/docker-compose.yml"},
		"kubernetes":     {"k8s/deployment.yaml"},
		"cloudformation": {"cloudformation/template.yaml"},
		"pulumi":         {"pulumi/stack.json"},
	}
	if len(reqs) != len(want) {
		t.Fatalf("detectAutoScanRequests returned %d request(s), want %d: %+v", len(reqs), len(want), reqs)
	}
	for _, req := range reqs {
		paths, ok := want[req.Source]
		if !ok {
			t.Fatalf("unexpected source %q in %+v", req.Source, reqs)
		}
		if strings.Join(req.Paths, ",") != strings.Join(paths, ",") {
			t.Fatalf("source %s paths = %v, want %v", req.Source, req.Paths, paths)
		}
	}
}

// --- graph nodes ---

func TestGraphNodesCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphNodesCmd(), "nodes")
	if err != nil {
		t.Fatalf("graph nodes error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "vm:web1") {
		t.Errorf("expected 'vm:web1' in output, got: %s", output)
	}
	if !strings.Contains(output, "db:pg1") {
		t.Errorf("expected 'db:pg1' in output, got: %s", output)
	}
}

func TestGraphNodesCmd_Filter(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphNodesCmd(), "nodes", "--type", string(models.AssetVM))
	if err != nil {
		t.Fatalf("graph nodes --type error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "vm:web1") {
		t.Errorf("expected 'vm:web1' in output, got: %s", output)
	}
	if strings.Contains(output, "db:pg1") {
		t.Errorf("db:pg1 should be filtered out, got: %s", output)
	}
}

// --- graph edges ---

func TestGraphEdgesCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphEdgesCmd(), "edges")
	if err != nil {
		t.Fatalf("graph edges error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "vm:web1") {
		t.Errorf("expected 'vm:web1' in edges output, got: %s", output)
	}
	if !strings.Contains(output, "db:pg1") {
		t.Errorf("expected 'db:pg1' in edges output, got: %s", output)
	}
}

// --- graph neighbors ---

func TestGraphNeighborsCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphNeighborsCmd(), "neighbors", "vm:web1")
	if err != nil {
		t.Fatalf("graph neighbors error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "db:pg1") {
		t.Errorf("expected neighbor 'db:pg1' in output, got: %s", output)
	}
}

func TestGraphNeighborsCmd_NotFound(t *testing.T) {
	app, _ := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphNeighborsCmd(), "neighbors", "nonexistent:node")
	if err == nil {
		t.Error("expected error for nonexistent node")
	}
}

// --- graph export ---

func TestGraphExportCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphExportCmd(), "export", "--format", "json")
	if err != nil {
		t.Fatalf("graph export json error: %v", err)
	}

	output := buf.String()
	// Validate it's valid JSON
	var parsed interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("export JSON is not valid JSON: %v\nOutput: %s", err, output)
	}
}

func TestGraphExportCmd_DOT(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphExportCmd(), "export", "--format", "dot")
	if err != nil {
		t.Fatalf("graph export dot error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "digraph") {
		t.Errorf("export DOT should contain 'digraph', got: %s", output)
	}
}

// --- graph path ---

func TestGraphPathCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphPathCmd(), "path", "vm:web1", "db:pg1")
	if err != nil {
		t.Fatalf("graph path error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Shortest path") {
		t.Errorf("expected 'Shortest path' in output, got: %s", output)
	}
}

// --- graph deps ---

func TestGraphDepsCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphDepsCmd(), "deps", "vm:web1")
	if err != nil {
		t.Fatalf("graph deps error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Dependencies of") {
		t.Errorf("expected 'Dependencies of' in output, got: %s", output)
	}
}

// --- graph cycles ---

func TestGraphCyclesCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphCyclesCmd(), "cycles")
	if err != nil {
		t.Fatalf("graph cycles error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No circular dependencies found.") {
		t.Errorf("expected no cycles message, got: %s", output)
	}
}

// --- graph spof ---

func TestGraphSPOFCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphSPOFCmd(), "spof")
	if err != nil {
		t.Fatalf("graph spof error: %v", err)
	}

	output := buf.String()
	// With 2 nodes and 1 edge, there may or may not be SPOFs depending on direction
	if output == "" {
		t.Error("expected some output from spof command")
	}
}

// --- graph orphans ---

func TestGraphOrphansCmd(t *testing.T) {
	app, buf := newTestApp(t)
	// Seed data, then add an orphan node
	seedTestData(t, app)

	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	_ = store.UpsertNode(context.Background(), models.Node{
		ID: "orphan:lonely", Name: "lonely", Type: models.AssetVM,
		Source: "terraform", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	})
	_ = store.Close()

	err = runCmd(app, app.graphOrphansCmd(), "orphans")
	if err != nil {
		t.Fatalf("graph orphans error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "orphan:lonely") {
		t.Errorf("expected 'orphan:lonely' in output, got: %s", output)
	}
}

// --- graph prune ---

func TestGraphPruneCmd_Force(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphPruneCmd(), "prune", "--source", "terraform", "--force")
	if err != nil {
		t.Fatalf("graph prune error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Deleted") {
		t.Errorf("expected 'Deleted' in output, got: %s", output)
	}
}

func TestGraphPruneCmd_NoFilter(t *testing.T) {
	app, _ := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphPruneCmd(), "prune")
	if err == nil {
		t.Error("expected error when no filter is specified")
	}
}

// --- impact node ---

func TestImpactNodeCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.impactCmd(), "impact", "node", "db:pg1")
	if err != nil {
		t.Fatalf("impact node error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Impact Analysis") {
		t.Errorf("expected 'Impact Analysis' in output, got: %s", output)
	}
	if !strings.Contains(output, "Blast Radius") {
		t.Errorf("expected 'Blast Radius' in output, got: %s", output)
	}
}

func TestImpactNodeCmd_NotFound(t *testing.T) {
	app, _ := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.impactCmd(), "impact", "node", "nonexistent:node")
	if err == nil {
		t.Error("expected error for nonexistent node")
	}
}

// --- scan commands (real fixtures) ---

func TestScanTerraformCmd(t *testing.T) {
	app, buf := newTestApp(t)

	fixture, err := filepath.Abs("../../testdata/terraform/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	err = runCmd(app, app.scanCmd(), "scan", "terraform", fixture)
	if err != nil {
		t.Fatalf("scan terraform error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Discovered") {
		t.Errorf("expected 'Discovered' in output, got: %s", output)
	}
}

func TestScanCloudFormationCmd(t *testing.T) {
	app, buf := newTestApp(t)

	fixture, err := filepath.Abs("../../testdata/cloudformation/template.yaml")
	if err != nil {
		t.Fatal(err)
	}

	err = runCmd(app, app.scanCmd(), "scan", "cloudformation", fixture)
	if err != nil {
		t.Fatalf("scan cloudformation error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Discovered") {
		t.Errorf("expected 'Discovered' in output, got: %s", output)
	}
}

func TestScanPulumiCmd(t *testing.T) {
	app, buf := newTestApp(t)

	fixture, err := filepath.Abs("../../internal/parser/pulumi/testdata/simple.json")
	if err != nil {
		t.Fatal(err)
	}

	err = runCmd(app, app.scanCmd(), "scan", "pulumi", fixture)
	if err != nil {
		t.Fatalf("scan pulumi error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Discovered") {
		t.Errorf("expected 'Discovered' in output, got: %s", output)
	}
}

// --- db stats ---

func TestDBStatsCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.dbCmd(), "db", "stats")
	if err != nil {
		t.Fatalf("db stats error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Nodes: 2") {
		t.Errorf("expected 'Nodes: 2' in output, got: %s", output)
	}
}

// --- certs commands ---

func TestCertsListCmd_Empty(t *testing.T) {
	app, buf := newTestApp(t)

	err := runCmd(app, app.certsCmd(), "certs", "list")
	if err != nil {
		t.Fatalf("certs list error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No certificates found") {
		t.Errorf("expected 'No certificates found' in output, got: %s", output)
	}
}

func TestCertsExpiringCmd_Empty(t *testing.T) {
	app, buf := newTestApp(t)

	err := runCmd(app, app.certsCmd(), "certs", "expiring")
	if err != nil {
		t.Fatalf("certs expiring error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No certificates expiring") {
		t.Errorf("expected 'No certificates expiring' in output, got: %s", output)
	}
}

// --- more scan commands ---

func TestScanAnsibleCmd(t *testing.T) {
	app, buf := newTestApp(t)

	fixture, err := filepath.Abs("../../testdata/ansible/inventory.yml")
	if err != nil {
		t.Fatal(err)
	}

	err = runCmd(app, app.scanCmd(), "scan", "ansible", fixture)
	if err != nil {
		t.Fatalf("scan ansible error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Discovered") {
		t.Errorf("expected 'Discovered' in output, got: %s", output)
	}
}

func TestScanK8sCmd(t *testing.T) {
	app, buf := newTestApp(t)

	fixture, err := filepath.Abs("../../testdata/kubernetes/manifests.yaml")
	if err != nil {
		t.Fatal(err)
	}

	err = runCmd(app, app.scanCmd(), "scan", "kubernetes", fixture)
	if err != nil {
		t.Fatalf("scan kubernetes error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Discovered") {
		t.Errorf("expected 'Discovered' in output, got: %s", output)
	}
}

func TestScanComposeCmd(t *testing.T) {
	app, buf := newTestApp(t)

	fixture, err := filepath.Abs("../../testdata/compose/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}

	err = runCmd(app, app.scanCmd(), "scan", "compose", fixture)
	if err != nil {
		t.Fatalf("scan compose error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Discovered") {
		t.Errorf("expected 'Discovered' in output, got: %s", output)
	}
}

func TestScanTerraformPlanCmd(t *testing.T) {
	app, buf := newTestApp(t)

	fixture, err := filepath.Abs("../../internal/parser/terraform/testdata/plan_create.json")
	if err != nil {
		t.Fatal(err)
	}

	err = runCmd(app, app.scanCmd(), "scan", "terraform-plan", fixture)
	if err != nil {
		t.Fatalf("scan terraform-plan error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Discovered") {
		t.Errorf("expected 'Discovered' in output, got: %s", output)
	}
}

// --- graph export mermaid ---

func TestGraphExportCmd_Mermaid(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphExportCmd(), "export", "--format", "mermaid")
	if err != nil {
		t.Fatalf("graph export mermaid error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "graph") {
		t.Errorf("export mermaid should contain 'graph', got: %s", output)
	}
}

// --- db backup ---

func TestDBBackupCmd(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	err := runCmd(app, app.dbCmd(), "db", "backup", backupPath)
	if err != nil {
		t.Fatalf("db backup error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Backed up") {
		t.Errorf("expected 'Backed up' in output, got: %s", output)
	}

	// Verify backup file exists
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("backup file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("backup file is empty")
	}
}

// --- graph prune confirmation ---

func TestGraphPruneCmd_Confirm_Yes(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	app.in = strings.NewReader("y\n")
	err := runCmd(app, app.graphPruneCmd(), "prune", "--source", "terraform")
	if err != nil {
		t.Fatalf("graph prune error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Deleted") {
		t.Errorf("expected 'Deleted' in output, got: %s", output)
	}
}

func TestGraphPruneCmd_Confirm_No(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	app.in = strings.NewReader("n\n")
	err := runCmd(app, app.graphPruneCmd(), "prune", "--source", "terraform")
	if err != nil {
		t.Fatalf("graph prune error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Aborted") {
		t.Errorf("expected 'Aborted' in output, got: %s", output)
	}
}

func TestGraphPruneCmd_NoMatchingNodes(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphPruneCmd(), "prune", "--source", "nonexistent-source")
	if err != nil {
		t.Fatalf("graph prune error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No matching nodes found") {
		t.Errorf("expected 'No matching nodes found' in output, got: %s", output)
	}
}

func TestGraphExportCmd_InvalidFormat(t *testing.T) {
	app, _ := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphExportCmd(), "export", "--format", "invalid")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestDBBackupCmd_Overwrite_No(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	// Create file first
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := os.WriteFile(backupPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	app.in = strings.NewReader("n\n")
	err := runCmd(app, app.dbCmd(), "db", "backup", backupPath)
	if err != nil {
		t.Fatalf("db backup error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Aborted") {
		t.Errorf("expected 'Aborted' in output, got: %s", output)
	}
}

func TestDBBackupCmd_Overwrite_Yes(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	// Create file first
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := os.WriteFile(backupPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	app.in = strings.NewReader("y\n")
	err := runCmd(app, app.dbCmd(), "db", "backup", backupPath)
	if err != nil {
		t.Fatalf("db backup error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Backed up") {
		t.Errorf("expected 'Backed up' in output, got: %s", output)
	}
}

func TestScanTerraformPlanCmd_Realistic(t *testing.T) {
	app, buf := newTestApp(t)

	fixture, err := filepath.Abs("../../internal/parser/terraform/testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	err = runCmd(app, app.scanCmd(), "scan", "terraform-plan", fixture)
	if err != nil {
		t.Fatalf("scan terraform-plan error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "11 nodes") {
		t.Errorf("expected '11 nodes' in output, got: %s", output)
	}
}

func TestPrintScanResult_WithDrift(t *testing.T) {
	app, buf := newTestApp(t)
	app.printScanResult(scanner.ScanResult{
		ScanID:     1,
		NodesFound: 5,
		EdgesFound: 3,
		Drift: &graph.DriftSummary{
			IsInitial: true,
		},
	})

	output := buf.String()
	if !strings.Contains(output, "initial scan") {
		t.Errorf("expected 'initial scan' in output, got: %s", output)
	}
}

func TestPrintScanResult_WithDriftChanges(t *testing.T) {
	app, buf := newTestApp(t)
	app.printScanResult(scanner.ScanResult{
		ScanID:     1,
		NodesFound: 5,
		EdgesFound: 3,
		Drift: &graph.DriftSummary{
			NodesAdded: []graph.NodeChange{{ID: "new:node"}},
		},
	})

	output := buf.String()
	if !strings.Contains(output, "Drift:") {
		t.Errorf("expected 'Drift:' in output, got: %s", output)
	}
}

func TestPrintScanResult_NoDrift(t *testing.T) {
	app, buf := newTestApp(t)
	app.printScanResult(scanner.ScanResult{
		ScanID:     1,
		NodesFound: 5,
		EdgesFound: 3,
		Drift:      &graph.DriftSummary{},
	})

	output := buf.String()
	if !strings.Contains(output, "No drift detected") {
		t.Errorf("expected 'No drift detected' in output, got: %s", output)
	}
}

func TestCertsListCmd_WithCerts(t *testing.T) {
	app, buf := newTestApp(t)
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	expires := now.Add(30 * 24 * time.Hour)
	_ = store.UpsertNode(ctx, models.Node{
		ID: "cert:test", Name: "test-cert", Type: models.AssetCertificate,
		Source: "probe", Metadata: map[string]string{},
		ExpiresAt: &expires, LastSeen: now, FirstSeen: now,
	})
	_ = store.Close()

	err = runCmd(app, app.certsCmd(), "certs", "list")
	if err != nil {
		t.Fatalf("certs list error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "test-cert") {
		t.Errorf("expected 'test-cert' in output, got: %s", output)
	}
}

func TestCertsExpiringCmd_WithExpiring(t *testing.T) {
	app, buf := newTestApp(t)
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	expires := now.Add(10 * 24 * time.Hour)
	_ = store.UpsertNode(ctx, models.Node{
		ID: "cert:expiring", Name: "expiring-cert", Type: models.AssetCertificate,
		Source: "probe", Metadata: map[string]string{},
		ExpiresAt: &expires, LastSeen: now, FirstSeen: now,
	})
	_ = store.Close()

	err = runCmd(app, app.certsCmd(), "certs", "expiring", "--days", "30")
	if err != nil {
		t.Fatalf("certs expiring error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "expiring-cert") {
		t.Errorf("expected 'expiring-cert' in output, got: %s", output)
	}
}

func TestGraphDepsCmd_NoDeps(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	// db:pg1 has no outgoing dependencies (it's the leaf)
	err := runCmd(app, app.graphDepsCmd(), "deps", "db:pg1")
	if err != nil {
		t.Fatalf("graph deps error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No dependencies found") {
		t.Errorf("expected 'No dependencies found' in output, got: %s", output)
	}
}

func TestGraphCyclesCmd_WithCycle(t *testing.T) {
	app, buf := newTestApp(t)
	// Seed a 3-node cycle: A -> B -> C -> A
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	for _, id := range []string{"A", "B", "C"} {
		_ = store.UpsertNode(ctx, models.Node{
			ID: id, Name: id, Type: models.AssetVM,
			Source: "tf", Metadata: map[string]string{},
			LastSeen: now, FirstSeen: now,
		})
	}
	_ = store.UpsertEdge(ctx, models.Edge{ID: "A->B", FromID: "A", ToID: "B", Type: models.EdgeDependsOn, Metadata: map[string]string{}})
	_ = store.UpsertEdge(ctx, models.Edge{ID: "B->C", FromID: "B", ToID: "C", Type: models.EdgeDependsOn, Metadata: map[string]string{}})
	_ = store.UpsertEdge(ctx, models.Edge{ID: "C->A", FromID: "C", ToID: "A", Type: models.EdgeDependsOn, Metadata: map[string]string{}})
	_ = store.Close()

	err = runCmd(app, app.graphCyclesCmd(), "cycles")
	if err != nil {
		t.Fatalf("graph cycles error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "circular dependency") {
		t.Errorf("expected 'circular dependency' in output, got: %s", output)
	}
}

func TestGraphOrphansCmd_WithOrphans(t *testing.T) {
	app, buf := newTestApp(t)
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	// Create 3 nodes, only 2 connected
	_ = store.UpsertNode(ctx, models.Node{ID: "A", Name: "A", Type: models.AssetVM, Source: "tf", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertNode(ctx, models.Node{ID: "B", Name: "B", Type: models.AssetVM, Source: "tf", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertNode(ctx, models.Node{ID: "C", Name: "C", Type: models.AssetVM, Source: "tf", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertEdge(ctx, models.Edge{ID: "A->B", FromID: "A", ToID: "B", Type: models.EdgeDependsOn, Metadata: map[string]string{}})
	_ = store.Close()

	err = runCmd(app, app.graphOrphansCmd(), "orphans")
	if err != nil {
		t.Fatalf("graph orphans error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "orphan") {
		t.Errorf("expected 'orphan' in output, got: %s", output)
	}
	if !strings.Contains(output, "C") {
		t.Errorf("expected orphan node C in output, got: %s", output)
	}
}

func TestGraphOrphansCmd_NoOrphans(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphOrphansCmd(), "orphans")
	if err != nil {
		t.Fatalf("graph orphans error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No orphan") {
		t.Errorf("expected 'No orphan' in output, got: %s", output)
	}
}

func TestGraphSPOFCmd_WithSPOF(t *testing.T) {
	app, buf := newTestApp(t)
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	// Create a hub that multiple nodes depend on
	_ = store.UpsertNode(ctx, models.Node{ID: "hub", Name: "hub", Type: models.AssetNetwork, Source: "tf", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("spoke-%d", i)
		_ = store.UpsertNode(ctx, models.Node{ID: id, Name: id, Type: models.AssetVM, Source: "tf", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
		_ = store.UpsertEdge(ctx, models.Edge{ID: id + "->hub", FromID: id, ToID: "hub", Type: models.EdgeDependsOn, Metadata: map[string]string{}})
	}
	_ = store.Close()

	err = runCmd(app, app.graphSPOFCmd(), "spof")
	if err != nil {
		t.Fatalf("graph spof error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "single point") {
		t.Errorf("expected 'single point' in output, got: %s", output)
	}
}

func TestGraphSPOFCmd_NoSPOF(t *testing.T) {
	app, buf := newTestApp(t)
	// Empty store — no nodes, no SPOF
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	err = runCmd(app, app.graphSPOFCmd(), "spof")
	if err != nil {
		t.Fatalf("graph spof error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No single points of failure") {
		t.Errorf("expected 'No single points of failure' in output, got: %s", output)
	}
}

func TestScanK8sCmd_NoArgs(t *testing.T) {
	app, _ := newTestApp(t)

	err := runCmd(app, app.scanK8sCmd(), "kubernetes")
	if err == nil {
		t.Error("expected error when no path and no --live flag")
	}
}

func TestImpactNodeCmd_WithWarnings(t *testing.T) {
	app, buf := newTestApp(t)
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	expires := now.Add(10 * 24 * time.Hour)

	_ = store.UpsertNode(ctx, models.Node{ID: "lb:main", Name: "main-lb", Type: models.AssetLoadBalancer, Source: "tf", Provider: "aws", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertNode(ctx, models.Node{ID: "cert:expiring", Name: "expiring-cert", Type: models.AssetCertificate, Source: "tf", Provider: "tls", ExpiresAt: &expires, Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
	_ = store.UpsertEdge(ctx, models.Edge{ID: "cert->lb", FromID: "cert:expiring", ToID: "lb:main", Type: models.EdgeDependsOn, Metadata: map[string]string{}})
	_ = store.Close()

	err = runCmd(app, app.impactNodeCmd(), "node", "lb:main")
	if err != nil {
		t.Fatalf("impact node error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Impact Analysis") {
		t.Errorf("expected 'Impact Analysis' in output, got: %s", output)
	}
	if !strings.Contains(output, "expires") {
		t.Errorf("expected 'expires' warning in output, got: %s", output)
	}
}

func TestScanK8sCmd_LiveFlag(t *testing.T) {
	app, _ := newTestApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// --live without a real cluster should not hang and should return promptly.
	err := runCmdWithContext(ctx, app, app.scanK8sCmd(), "kubernetes", "--live")
	if err == nil {
		t.Log("scan k8s --live succeeded unexpectedly (likely local cluster present)")
	}
}

func TestGraphSPOFCmd_WithLimit(t *testing.T) {
	app, buf := newTestApp(t)
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	// Create two hubs
	for _, hub := range []string{"hub1", "hub2"} {
		_ = store.UpsertNode(ctx, models.Node{ID: hub, Name: hub, Type: models.AssetNetwork, Source: "tf", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
		for i := 0; i < 3; i++ {
			id := fmt.Sprintf("%s-spoke-%d", hub, i)
			_ = store.UpsertNode(ctx, models.Node{ID: id, Name: id, Type: models.AssetVM, Source: "tf", Metadata: map[string]string{}, LastSeen: now, FirstSeen: now})
			_ = store.UpsertEdge(ctx, models.Edge{ID: id + "->" + hub, FromID: id, ToID: hub, Type: models.EdgeDependsOn, Metadata: map[string]string{}})
		}
	}
	_ = store.Close()

	err = runCmd(app, app.graphSPOFCmd(), "spof", "--limit", "1")
	if err != nil {
		t.Fatalf("graph spof error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "single point") {
		t.Errorf("expected SPOF output, got: %s", output)
	}
}

func TestDBStatsCmd_Empty(t *testing.T) {
	app, buf := newTestApp(t)
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	err = runCmd(app, app.dbStatsCmd(), "stats")
	if err != nil {
		t.Fatalf("db stats error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Nodes:") {
		t.Errorf("expected 'Nodes:' in output, got: %s", output)
	}
}

func TestGraphPathCmd_ToNotFound(t *testing.T) {
	app, _ := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphPathCmd(), "path", "vm:web1", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent 'to' node")
	}
}

func TestGraphPathCmd_FromNotFound(t *testing.T) {
	app, _ := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphPathCmd(), "path", "nonexistent", "db:pg1")
	if err == nil {
		t.Error("expected error for nonexistent 'from' node")
	}
}

func TestGraphEdgesCmd_WithFilter(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphEdgesCmd(), "edges", "--type", "depends_on")
	if err != nil {
		t.Fatalf("graph edges error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "depends_on") {
		t.Errorf("expected 'depends_on' in output, got: %s", output)
	}
}

func TestGraphCmd_HasSubcommands(t *testing.T) {
	app, _ := newTestApp(t)
	cmd := app.graphCmd()
	if !cmd.HasSubCommands() {
		t.Error("graph command should have subcommands")
	}
	// Verify key subcommands are registered
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range []string{"show", "nodes", "edges", "path", "deps", "prune", "export", "cycles", "spof", "orphans"} {
		if !names[want] {
			t.Errorf("missing subcommand %q", want)
		}
	}
}

func TestGraphDepsCmd_NodeNotFound(t *testing.T) {
	app, _ := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphDepsCmd(), "deps", "nonexistent:node")
	if err == nil {
		t.Error("expected error for nonexistent node")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestDBStatsCmd_WithScans(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	// Record a scan so the scan status count loop is exercised.
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	scanID, err := store.RecordScan(ctx, graph.Scan{
		Source:     "terraform",
		SourcePath: "/some/path",
		StartedAt:  time.Now(),
		Status:     "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateScan(ctx, scanID, "completed", 5, 3); err != nil {
		t.Fatal(err)
	}
	store.Close() //nolint:errcheck // best-effort cleanup in test

	if err := runCmd(app, app.dbStatsCmd(), "stats"); err != nil {
		t.Fatalf("db stats error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Scans:") {
		t.Error("expected 'Scans:' in output")
	}
	if !strings.Contains(output, "completed") {
		t.Errorf("expected 'completed' in scan status output, got: %s", output)
	}
}

func TestGraphPruneCmd_MoreThan10Nodes(t *testing.T) {
	app, buf := newTestApp(t)
	app.in = strings.NewReader("y\n")

	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	// Create 12 nodes so the "... and N more" branch is triggered.
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("vm:node%d", i)
		if err := store.UpsertNode(ctx, models.Node{
			ID: id, Name: fmt.Sprintf("node%d", i), Type: models.AssetVM,
			Source: "terraform", Provider: "aws", Metadata: map[string]string{},
			LastSeen: now.Add(-48 * time.Hour), FirstSeen: now.Add(-48 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	store.Close() //nolint:errcheck // best-effort cleanup in test

	err = runCmd(app, app.graphPruneCmd(), "prune", "--stale-days", "1")
	if err != nil {
		t.Fatalf("graph prune error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "... and") {
		t.Errorf("expected '... and' in output for >10 nodes, got: %s", output)
	}
}

func TestGraphNodesCmd_WithFilter(t *testing.T) {
	app, buf := newTestApp(t)
	seedTestData(t, app)

	err := runCmd(app, app.graphNodesCmd(), "nodes", "--type", "vm")
	if err != nil {
		t.Fatalf("graph nodes error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "web1") {
		t.Errorf("expected 'web1' in output, got: %s", output)
	}
}

// --- JSON output tests (--output=json) ---

func TestGraphShowCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"
	seedTestData(t, app)

	err := runCmd(app, app.graphShowCmd(), "show")
	if err != nil {
		t.Fatalf("graph show --output=json error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if result["total_nodes"].(float64) != 2 {
		t.Errorf("expected total_nodes=2, got %v", result["total_nodes"])
	}
	if result["total_edges"].(float64) != 1 {
		t.Errorf("expected total_edges=1, got %v", result["total_edges"])
	}
	if result["nodes_by_type"] == nil {
		t.Error("expected nodes_by_type in JSON output")
	}
	if result["edges_by_type"] == nil {
		t.Error("expected edges_by_type in JSON output")
	}
}

func TestGraphNodesCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"
	seedTestData(t, app)

	err := runCmd(app, app.graphNodesCmd(), "nodes")
	if err != nil {
		t.Fatalf("graph nodes --output=json error: %v", err)
	}

	var nodes []models.Node
	if err := json.Unmarshal(buf.Bytes(), &nodes); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	ids := map[string]bool{}
	for _, n := range nodes {
		ids[n.ID] = true
		if n.Name == "" {
			t.Error("expected non-empty node name")
		}
	}
	if !ids["vm:web1"] || !ids["db:pg1"] {
		t.Errorf("expected vm:web1 and db:pg1, got %v", ids)
	}
}

func TestGraphEdgesCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"
	seedTestData(t, app)

	err := runCmd(app, app.graphEdgesCmd(), "edges")
	if err != nil {
		t.Fatalf("graph edges --output=json error: %v", err)
	}

	var edges []models.Edge
	if err := json.Unmarshal(buf.Bytes(), &edges); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].FromID != "vm:web1" || edges[0].ToID != "db:pg1" {
		t.Errorf("unexpected edge: %s -> %s", edges[0].FromID, edges[0].ToID)
	}
}

func TestGraphNeighborsCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"
	seedTestData(t, app)

	err := runCmd(app, app.graphNeighborsCmd(), "neighbors", "vm:web1")
	if err != nil {
		t.Fatalf("graph neighbors --output=json error: %v", err)
	}

	var nodes []models.Node
	if err := json.Unmarshal(buf.Bytes(), &nodes); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 neighbor, got %d", len(nodes))
	}
	if nodes[0].ID != "db:pg1" {
		t.Errorf("expected neighbor db:pg1, got %s", nodes[0].ID)
	}
}

func TestGraphPathCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"
	seedTestData(t, app)

	err := runCmd(app, app.graphPathCmd(), "path", "vm:web1", "db:pg1")
	if err != nil {
		t.Fatalf("graph path --output=json error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if result["from"] != "vm:web1" {
		t.Errorf("expected from=vm:web1, got %v", result["from"])
	}
	if result["to"] != "db:pg1" {
		t.Errorf("expected to=db:pg1, got %v", result["to"])
	}
	if result["nodes"] == nil {
		t.Error("expected nodes in JSON output")
	}
}

func TestGraphDepsCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"
	seedTestData(t, app)

	err := runCmd(app, app.graphDepsCmd(), "deps", "vm:web1")
	if err != nil {
		t.Fatalf("graph deps --output=json error: %v", err)
	}

	var nodes []models.Node
	if err := json.Unmarshal(buf.Bytes(), &nodes); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	// vm:web1 depends on db:pg1
	if len(nodes) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(nodes))
	}
}

func TestGraphOrphansCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"

	// Seed an orphan node (no edges)
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now()
	_ = store.UpsertNode(ctx, models.Node{
		ID: "orphan:lonely", Name: "lonely", Type: models.AssetVM,
		Source: "test", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	})
	store.Close() //nolint:errcheck

	err = runCmd(app, app.graphOrphansCmd(), "orphans")
	if err != nil {
		t.Fatalf("graph orphans --output=json error: %v", err)
	}

	var nodes []models.Node
	if err := json.Unmarshal(buf.Bytes(), &nodes); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if len(nodes) != 1 || nodes[0].ID != "orphan:lonely" {
		t.Errorf("expected orphan node, got %v", nodes)
	}
}

func TestGraphCyclesCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"
	seedTestData(t, app)

	err := runCmd(app, app.graphCyclesCmd(), "cycles")
	if err != nil {
		t.Fatalf("graph cycles --output=json error: %v", err)
	}

	var cycles [][]string
	if err := json.Unmarshal(buf.Bytes(), &cycles); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	// no cycles in test data
	if len(cycles) != 0 {
		t.Errorf("expected 0 cycles, got %d", len(cycles))
	}
}

func TestGraphSPOFCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"
	seedTestData(t, app)

	err := runCmd(app, app.graphSPOFCmd(), "spof")
	if err != nil {
		t.Fatalf("graph spof --output=json error: %v", err)
	}

	var spofs []graph.SPOFNode
	if err := json.Unmarshal(buf.Bytes(), &spofs); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	// seed data has web1->pg1, so pg1 is a SPOF
	found := false
	for _, s := range spofs {
		if s.Node.ID == "db:pg1" {
			found = true
		}
	}
	if !found {
		t.Error("expected db:pg1 as SPOF")
	}
}

func TestImpactNodeCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"
	seedTestData(t, app)

	err := runCmd(app, app.impactCmd(), "impact", "node", "db:pg1")
	if err != nil {
		t.Fatalf("impact node --output=json error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if result["node_id"] != "db:pg1" {
		t.Errorf("expected node_id=db:pg1, got %v", result["node_id"])
	}
	if result["impact_tree"] == nil {
		t.Error("expected impact_tree in JSON output")
	}
	if result["blast_radius"] == nil {
		t.Error("expected blast_radius in JSON output")
	}
}

func TestDBStatsCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"
	seedTestData(t, app)

	err := runCmd(app, app.dbStatsCmd(), "stats")
	if err != nil {
		t.Fatalf("db stats --output=json error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if result["total_nodes"].(float64) != 2 {
		t.Errorf("expected total_nodes=2, got %v", result["total_nodes"])
	}
	if result["total_edges"].(float64) != 1 {
		t.Errorf("expected total_edges=1, got %v", result["total_edges"])
	}
	if result["path"] == nil {
		t.Error("expected path in JSON output")
	}
}

func TestVersionCmd_JSON(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"

	err := runCmd(app, app.versionCmd(), "version")
	if err != nil {
		t.Fatalf("version --output=json error: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if result["version"] != "test" {
		t.Errorf("expected version=test, got %q", result["version"])
	}
}

func TestCertsListCmd_JSON_Empty(t *testing.T) {
	app, buf := newTestApp(t)
	app.outputFormat = "json"

	// Init store so certs list can run
	store, _, err := app.openStore()
	if err != nil {
		t.Fatal(err)
	}
	store.Close() //nolint:errcheck

	err = runCmd(app, app.certsListCmd(), "list")
	if err != nil {
		t.Fatalf("certs list --output=json error: %v", err)
	}

	// Should produce valid JSON even when empty
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("expected valid JSON, got: %s", buf.String())
	}
}

package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/internal/scanner"
	"github.com/matijazezelj/aib/pkg/models"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

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

func TestVersionCmd(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := versionCmd()
	cmd.Run(cmd, nil)

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if output == "" {
		t.Error("version command produced no output")
	}
	if !strings.Contains(output, "aib") {
		t.Errorf("version output should contain 'aib', got %q", output)
	}
}

func TestCompletionCmd_Bash(t *testing.T) {
	root := &cobra.Command{Use: "aib"}
	root.AddCommand(completionCmd())

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root.SetArgs([]string{"completion", "bash"})
	err := root.Execute()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("completion bash error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("completion bash produced no output")
	}
}

func TestCompletionCmd_Zsh(t *testing.T) {
	root := &cobra.Command{Use: "aib"}
	root.AddCommand(completionCmd())

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root.SetArgs([]string{"completion", "zsh"})
	err := root.Execute()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("completion zsh error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("completion zsh produced no output")
	}
}

func TestCompletionCmd_Fish(t *testing.T) {
	root := &cobra.Command{Use: "aib"}
	root.AddCommand(completionCmd())

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root.SetArgs([]string{"completion", "fish"})
	err := root.Execute()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("completion fish error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("completion fish produced no output")
	}
}

func TestCompletionCmd_PowerShell(t *testing.T) {
	root := &cobra.Command{Use: "aib"}
	root.AddCommand(completionCmd())

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root.SetArgs([]string{"completion", "powershell"})
	err := root.Execute()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("completion powershell error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("completion powershell produced no output")
	}
}

func TestCompletionCmd_InvalidShell(t *testing.T) {
	root := &cobra.Command{Use: "aib"}
	root.AddCommand(completionCmd())

	root.SetArgs([]string{"completion", "invalid"})
	err := root.Execute()
	if err == nil {
		t.Error("expected error for invalid shell")
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

func TestPrintScanResult_Success(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printScanResult(scanner.ScanResult{
		ScanID:     1,
		NodesFound: 10,
		EdgesFound: 5,
		Warnings:   []string{"missing provider"},
	})

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
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
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printScanResult(scanner.ScanResult{
		Error: fmt.Errorf("scan failed"),
	})

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "failed") {
		t.Errorf("output should mention failure, got: %s", output)
	}
}

func TestPrintTree(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

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

	printTree(context.Background(), tree, "  ", true)

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "root") {
		t.Errorf("output should contain root, got: %s", output)
	}
	if !strings.Contains(output, "child1") {
		t.Errorf("output should contain child1, got: %s", output)
	}
}

func TestGraphShowCmd_WithPreseededDB(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	store, err := graph.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Truncate(time.Second)
	ctx := context.Background()
	_ = store.UpsertNode(ctx, models.Node{
		ID: "vm:web1", Name: "web1", Type: models.AssetVM,
		Source: "terraform", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	})
	_ = store.UpsertNode(ctx, models.Node{
		ID: "db:pg1", Name: "pg1", Type: models.AssetDatabase,
		Source: "terraform", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	})
	_ = store.Close()

	nodeCount := 2

	// Re-read from the file to verify
	store2, err := graph.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = store2.Init(ctx)
	nc, _ := store2.NodeCount(ctx)
	_ = store2.Close()
	if nc != nodeCount {
		t.Fatalf("expected %d nodes in pre-seeded DB, got %d", nodeCount, nc)
	}
}

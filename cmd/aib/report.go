package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/internal/scanner"
	"github.com/matijazezelj/aib/pkg/models"
	"github.com/spf13/cobra"
)

type infrastructureReport struct {
	GeneratedAt      time.Time                    `json:"generated_at"`
	TotalNodes       int                          `json:"total_nodes"`
	TotalEdges       int                          `json:"total_edges"`
	NodesByType      map[string]int               `json:"nodes_by_type"`
	EdgesByType      map[string]int               `json:"edges_by_type"`
	NodesBySource    map[string]int               `json:"nodes_by_source"`
	Scans            []graph.Scan                 `json:"scans"`
	Audit            *graph.AuditReport           `json:"audit"`
	CorrelatedAssets []graph.CorrelatedAssetGroup `json:"correlated_assets"`
	SampleNodes      []models.Node                `json:"sample_nodes"`
}

func (a *cliApp) reportCmd() *cobra.Command {
	var format string
	var outPath string
	var maxNodes int

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate an infrastructure report from the current graph",
		Long:  "Generates Markdown or JSON suitable for CI artifacts and GitHub PR comments.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if format != "markdown" && format != "json" {
				return fmt.Errorf("invalid --format %q (use: markdown, json)", format)
			}
			store, _, err := a.openStore()
			if err != nil {
				return err
			}
			defer store.Close() //nolint:errcheck // best-effort cleanup

			report, err := buildInfrastructureReport(cmd.Context(), store, maxNodes)
			if err != nil {
				return err
			}

			var rendered []byte
			switch format {
			case "json":
				rendered, err = json.MarshalIndent(report, "", "  ")
			case "markdown":
				rendered = []byte(renderInfrastructureMarkdown(report))
			}
			if err != nil {
				return err
			}
			rendered = append(rendered, '\n')

			if outPath != "" {
				if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil && filepath.Dir(outPath) != "." {
					return err
				}
				return os.WriteFile(outPath, rendered, 0o600)
			}
			_, err = a.out.Write(rendered)
			return err
		},
	}
	cmd.Flags().StringVar(&format, "format", "markdown", "report format: markdown, json")
	cmd.Flags().StringVar(&outPath, "out", "", "write report to file instead of stdout")
	cmd.Flags().IntVar(&maxNodes, "max-nodes", 20, "maximum sample nodes to include")
	return cmd
}

func buildInfrastructureReport(ctx context.Context, store *graph.SQLiteStore, maxNodes int) (*infrastructureReport, error) {
	nodeCount, err := store.NodeCount(ctx)
	if err != nil {
		return nil, err
	}
	edgeCount, err := store.EdgeCount(ctx)
	if err != nil {
		return nil, err
	}
	nodesByType, err := store.NodeCountByType(ctx)
	if err != nil {
		return nil, err
	}
	edgesByType, err := store.EdgeCountByType(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := store.ListNodes(ctx, graph.NodeFilter{})
	if err != nil {
		return nil, err
	}
	nodesBySource := map[string]int{}
	for _, node := range nodes {
		nodesBySource[node.Source]++
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	if maxNodes >= 0 && len(nodes) > maxNodes {
		nodes = nodes[:maxNodes]
	}
	scans, err := store.ListScans(ctx, 10)
	if err != nil {
		return nil, err
	}
	audit, err := graph.RunAudit(ctx, store)
	if err != nil {
		return nil, err
	}
	correlatedAssets, err := graph.ListCorrelatedAssetGroups(ctx, store)
	if err != nil {
		return nil, err
	}
	if correlatedAssets == nil {
		correlatedAssets = []graph.CorrelatedAssetGroup{}
	}
	return &infrastructureReport{
		GeneratedAt:      time.Now().UTC(),
		TotalNodes:       nodeCount,
		TotalEdges:       edgeCount,
		NodesByType:      nodesByType,
		EdgesByType:      edgesByType,
		NodesBySource:    nodesBySource,
		Scans:            scans,
		Audit:            audit,
		CorrelatedAssets: correlatedAssets,
		SampleNodes:      nodes,
	}, nil
}

func renderInfrastructureMarkdown(r *infrastructureReport) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# AIB Infrastructure Report")
	fmt.Fprintf(&b, "\nGenerated: `%s`\n", r.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintln(&b, "\n## Summary")
	fmt.Fprintf(&b, "\n- Total nodes: %d\n", r.TotalNodes)
	fmt.Fprintf(&b, "- Total edges: %d\n", r.TotalEdges)
	fmt.Fprintf(&b, "- Security findings: %d (critical: %d, warning: %d, info: %d)\n",
		r.Audit.Summary.Total, r.Audit.Summary.Critical, r.Audit.Summary.Warning, r.Audit.Summary.Info)

	writeCountTable(&b, "Nodes by type", r.NodesByType)
	writeCountTable(&b, "Nodes by source", r.NodesBySource)
	writeCountTable(&b, "Edges by type", r.EdgesByType)

	fmt.Fprintln(&b, "\n## Correlated Assets")
	if len(r.CorrelatedAssets) == 0 {
		fmt.Fprintln(&b, "\nNo cross-source asset correlations found.")
	} else {
		fmt.Fprintln(&b, "\n| Identity | Sources | Assets |")
		fmt.Fprintln(&b, "|---|---|---|")
		for _, group := range r.CorrelatedAssets {
			assets := make([]string, 0, len(group.Nodes))
			for _, node := range group.Nodes {
				assets = append(assets, escapeMarkdownTable(fmt.Sprintf("%s:%s:%s", node.Source, node.Type, node.Name)))
			}
			fmt.Fprintf(&b, "| `%s` | %s | `%s` |\n", escapeMarkdownTable(group.Key), escapeMarkdownTable(strings.Join(group.Sources, ", ")), strings.Join(assets, "`, `"))
		}
	}

	fmt.Fprintln(&b, "\n## Security Findings")
	if len(r.Audit.Findings) == 0 {
		fmt.Fprintln(&b, "\nNo security findings.")
	} else {
		fmt.Fprintln(&b, "\n| Severity | Rule | Resource | Description |")
		fmt.Fprintln(&b, "|---|---|---|---|")
		for _, f := range r.Audit.Findings {
			fmt.Fprintf(&b, "| %s | %s | `%s` | %s |\n", f.Severity, escapeMarkdownTable(f.Rule), escapeMarkdownTable(f.ResourceID), escapeMarkdownTable(f.Description))
		}
	}

	fmt.Fprintln(&b, "\n## Recent Scans")
	if len(r.Scans) == 0 {
		fmt.Fprintln(&b, "\nNo scans recorded.")
	} else {
		fmt.Fprintln(&b, "\n| Source | Path | Status | Nodes | Edges |")
		fmt.Fprintln(&b, "|---|---|---|---:|---:|")
		for _, s := range r.Scans {
			fmt.Fprintf(&b, "| %s | `%s` | %s | %d | %d |\n", escapeMarkdownTable(s.Source), escapeMarkdownTable(s.SourcePath), escapeMarkdownTable(s.Status), s.NodesFound, s.EdgesFound)
		}
	}

	fmt.Fprintln(&b, "\n## Sample Nodes")
	if len(r.SampleNodes) == 0 {
		fmt.Fprintln(&b, "\nNo nodes found.")
	} else {
		fmt.Fprintln(&b, "\n| ID | Type | Source | Provider |")
		fmt.Fprintln(&b, "|---|---|---|---|")
		for _, n := range r.SampleNodes {
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", escapeMarkdownTable(n.ID), escapeMarkdownTable(string(n.Type)), escapeMarkdownTable(n.Source), escapeMarkdownTable(n.Provider))
		}
	}
	return b.String()
}

func writeCountTable(b *strings.Builder, title string, counts map[string]int) {
	fmt.Fprintf(b, "\n## %s\n", title)
	if len(counts) == 0 {
		fmt.Fprintln(b, "\nNone.")
		return
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintln(b, "\n| Name | Count |")
	fmt.Fprintln(b, "|---|---:|")
	for _, k := range keys {
		fmt.Fprintf(b, "| %s | %d |\n", escapeMarkdownTable(k), counts[k])
	}
}

func escapeMarkdownTable(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func (a *cliApp) scanAutoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "auto <path> [path...]",
		Short: "Auto-detect and scan supported infrastructure files",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reqs := detectAutoScanRequests(args)
			if len(reqs) == 0 {
				return fmt.Errorf("no supported infrastructure files detected")
			}
			store, cfg, err := a.openStore()
			if err != nil {
				return err
			}
			defer store.Close() //nolint:errcheck // best-effort cleanup
			sc := scanner.New(store, cfg, a.logger)
			for _, req := range reqs {
				_, _ = fmt.Fprintf(a.out, "Scanning %s across %d path(s)...\n", req.Source, len(req.Paths))
				result := sc.RunSync(cmd.Context(), req)
				a.printScanResult(result)
				if result.Error != nil {
					return result.Error
				}
			}
			return nil
		},
	}
}

func detectAutoScanRequests(paths []string) []scanner.ScanRequest {
	groups := map[string][]string{}
	for _, input := range paths {
		for _, path := range expandAutoScanPath(input) {
			if source := detectSourceForPath(path); source != "" {
				groups[source] = append(groups[source], path)
			}
		}
	}
	order := []string{"terraform", "terraform-plan", "kubernetes", "compose", "cloudformation", "pulumi", "ansible"}
	var reqs []scanner.ScanRequest
	for _, source := range order {
		if len(groups[source]) == 0 {
			continue
		}
		reqs = append(reqs, scanner.ScanRequest{Source: source, Paths: dedupeStrings(groups[source])})
	}
	return reqs
}

func expandAutoScanPath(input string) []string {
	info, err := os.Stat(input)
	if err != nil || !info.IsDir() {
		return []string{input}
	}
	var paths []string
	_ = filepath.WalkDir(input, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "node_modules" || base == "vendor" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if detectSourceForPath(path) != "" {
			paths = append(paths, path)
		}
		return nil
	})
	return paths
}

func detectSourceForPath(path string) string {
	lower := strings.ToLower(filepath.ToSlash(path))
	base := filepath.Base(lower)
	switch {
	case strings.HasSuffix(base, ".tfstate") || base == "terraform.tfstate":
		return "terraform"
	case strings.Contains(base, "tfplan") && strings.HasSuffix(base, ".json"):
		return "terraform-plan"
	case strings.Contains(base, "docker-compose") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")):
		return "compose"
	case strings.Contains(lower, "cloudformation") || strings.Contains(lower, "/cfn/"):
		if strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".json") {
			return "cloudformation"
		}
	case strings.Contains(lower, "pulumi") && strings.HasSuffix(base, ".json"):
		return "pulumi"
	case strings.Contains(lower, "ansible") && (strings.HasSuffix(base, ".ini") || strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")):
		return "ansible"
	case strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml"):
		return "kubernetes"
	}
	return ""
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

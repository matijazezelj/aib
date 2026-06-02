package main

import (
	"strings"
	"testing"
	"time"

	"github.com/matijazezelj/aib/internal/graph"
)

func TestBuildReportDiff(t *testing.T) {
	baseline := &infrastructureReport{
		Assets: []reportAsset{
			{ID: "compose:container:api", Name: "api", Type: "container", Source: "compose", Metadata: map[string]string{"image": "api:1.0"}},
			{ID: "compose:container:old", Name: "old", Type: "container", Source: "compose"},
		},
		Edges: []reportEdge{
			{ID: "api->depends_on->db", FromID: "api", ToID: "db", Type: "depends_on"},
		},
		Audit: &graph.AuditReport{Findings: []graph.Finding{
			{Severity: graph.SeverityWarning, Rule: "old-rule", ResourceID: "compose:container:old"},
		}},
	}
	current := &infrastructureReport{
		Assets: []reportAsset{
			{ID: "compose:container:api", Name: "api", Type: "container", Source: "compose", Metadata: map[string]string{"image": "api:1.1"}},
			{ID: "compose:container:new", Name: "new", Type: "container", Source: "compose"},
		},
		Edges: []reportEdge{
			{ID: "api->depends_on->cache", FromID: "api", ToID: "cache", Type: "depends_on"},
		},
		Audit: &graph.AuditReport{Findings: []graph.Finding{
			{Severity: graph.SeverityCritical, Rule: "new-rule", ResourceID: "compose:container:new"},
		}},
	}

	diff := buildReportDiff(current, baseline, "baseline.json")
	if diff.Summary.AddedAssets != 1 || diff.Summary.RemovedAssets != 1 || diff.Summary.ChangedAssets != 1 {
		t.Fatalf("asset diff summary = %+v", diff.Summary)
	}
	if diff.Summary.AddedEdges != 1 || diff.Summary.RemovedEdges != 1 || diff.Summary.ChangedEdges != 0 {
		t.Fatalf("edge diff summary = %+v", diff.Summary)
	}
	if diff.Summary.AddedFindings != 1 || diff.Summary.ResolvedFindings != 1 {
		t.Fatalf("findings diff summary = %+v", diff.Summary)
	}
	if diff.Assets.Added[0].ID != "compose:container:new" {
		t.Errorf("added asset = %q", diff.Assets.Added[0].ID)
	}
}

func TestRenderInfrastructureMarkdownIncludesDiff(t *testing.T) {
	report := &infrastructureReport{
		GeneratedAt:   mustParseTime(t, "2026-01-01T00:00:00Z"),
		NodesByType:   map[string]int{},
		EdgesByType:   map[string]int{},
		NodesBySource: map[string]int{},
		Audit:         &graph.AuditReport{},
		Diff: &reportDiff{
			BaselinePath: "baseline.json",
			Summary: diffSummary{
				AddedAssets:      1,
				RemovedAssets:    2,
				ChangedAssets:    3,
				AddedFindings:    4,
				ResolvedFindings: 5,
			},
			Assets:   assetDiff{Added: []reportAsset{{ID: "asset:new"}}},
			Findings: findingsDiff{Added: []findingKey{{Severity: "critical", Rule: "new-rule", ResourceID: "asset:new"}}},
		},
	}
	md := renderInfrastructureMarkdown(report)
	for _, want := range []string{"## Baseline Diff", "assets +1/-2/~3", "`asset:new`", "critical:new-rule:asset:new"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

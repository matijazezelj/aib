package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type reportAsset struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Type     string            `json:"type,omitempty"`
	Source   string            `json:"source,omitempty"`
	Provider string            `json:"provider,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type reportEdge struct {
	ID       string            `json:"id"`
	FromID   string            `json:"from_id"`
	ToID     string            `json:"to_id"`
	Type     string            `json:"type"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type reportDiff struct {
	BaselinePath string       `json:"baseline_path"`
	Summary      diffSummary  `json:"summary"`
	Assets       assetDiff    `json:"assets"`
	Edges        edgeDiff     `json:"edges"`
	Findings     findingsDiff `json:"findings"`
}

type diffSummary struct {
	AddedAssets      int `json:"added_assets"`
	RemovedAssets    int `json:"removed_assets"`
	ChangedAssets    int `json:"changed_assets"`
	AddedEdges       int `json:"added_edges"`
	RemovedEdges     int `json:"removed_edges"`
	ChangedEdges     int `json:"changed_edges"`
	AddedFindings    int `json:"added_findings"`
	ResolvedFindings int `json:"resolved_findings"`
}

type assetDiff struct {
	Added   []reportAsset `json:"added"`
	Removed []reportAsset `json:"removed"`
	Changed []assetChange `json:"changed"`
}

type edgeDiff struct {
	Added   []reportEdge `json:"added"`
	Removed []reportEdge `json:"removed"`
	Changed []edgeChange `json:"changed"`
}

type findingsDiff struct {
	Added    []findingKey `json:"added"`
	Resolved []findingKey `json:"resolved"`
}

type assetChange struct {
	Before reportAsset `json:"before"`
	After  reportAsset `json:"after"`
}

type edgeChange struct {
	Before reportEdge `json:"before"`
	After  reportEdge `json:"after"`
}

type findingKey struct {
	Severity   string `json:"severity"`
	Rule       string `json:"rule"`
	ResourceID string `json:"resource_id"`
}

func loadBaselineReport(path string) (*infrastructureReport, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- user supplied baseline report path
	if err != nil {
		return nil, err
	}
	var r infrastructureReport
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func buildReportDiff(current, baseline *infrastructureReport, baselinePath string) *reportDiff {
	d := &reportDiff{BaselinePath: baselinePath}
	d.Assets = diffAssets(current.Assets, baseline.Assets)
	d.Edges = diffEdges(current.Edges, baseline.Edges)
	d.Findings = diffFindings(current, baseline)
	d.Summary = diffSummary{
		AddedAssets:      len(d.Assets.Added),
		RemovedAssets:    len(d.Assets.Removed),
		ChangedAssets:    len(d.Assets.Changed),
		AddedEdges:       len(d.Edges.Added),
		RemovedEdges:     len(d.Edges.Removed),
		ChangedEdges:     len(d.Edges.Changed),
		AddedFindings:    len(d.Findings.Added),
		ResolvedFindings: len(d.Findings.Resolved),
	}
	return d
}

func diffAssets(current, baseline []reportAsset) assetDiff {
	cur := mapAssets(current)
	base := mapAssets(baseline)
	var d assetDiff
	for id, after := range cur {
		before, ok := base[id]
		if !ok {
			d.Added = append(d.Added, after)
			continue
		}
		if jsonFingerprint(before) != jsonFingerprint(after) {
			d.Changed = append(d.Changed, assetChange{Before: before, After: after})
		}
	}
	for id, before := range base {
		if _, ok := cur[id]; !ok {
			d.Removed = append(d.Removed, before)
		}
	}
	sort.Slice(d.Added, func(i, j int) bool { return d.Added[i].ID < d.Added[j].ID })
	sort.Slice(d.Removed, func(i, j int) bool { return d.Removed[i].ID < d.Removed[j].ID })
	sort.Slice(d.Changed, func(i, j int) bool { return d.Changed[i].After.ID < d.Changed[j].After.ID })
	return d
}

func diffEdges(current, baseline []reportEdge) edgeDiff {
	cur := mapEdges(current)
	base := mapEdges(baseline)
	var d edgeDiff
	for id, after := range cur {
		before, ok := base[id]
		if !ok {
			d.Added = append(d.Added, after)
			continue
		}
		if jsonFingerprint(before) != jsonFingerprint(after) {
			d.Changed = append(d.Changed, edgeChange{Before: before, After: after})
		}
	}
	for id, before := range base {
		if _, ok := cur[id]; !ok {
			d.Removed = append(d.Removed, before)
		}
	}
	sort.Slice(d.Added, func(i, j int) bool { return d.Added[i].ID < d.Added[j].ID })
	sort.Slice(d.Removed, func(i, j int) bool { return d.Removed[i].ID < d.Removed[j].ID })
	sort.Slice(d.Changed, func(i, j int) bool { return d.Changed[i].After.ID < d.Changed[j].After.ID })
	return d
}

func diffFindings(current, baseline *infrastructureReport) findingsDiff {
	cur := mapFindings(current)
	base := mapFindings(baseline)
	var d findingsDiff
	for key, f := range cur {
		if _, ok := base[key]; !ok {
			d.Added = append(d.Added, f)
		}
	}
	for key, f := range base {
		if _, ok := cur[key]; !ok {
			d.Resolved = append(d.Resolved, f)
		}
	}
	sort.Slice(d.Added, func(i, j int) bool { return findingSortKey(d.Added[i]) < findingSortKey(d.Added[j]) })
	sort.Slice(d.Resolved, func(i, j int) bool { return findingSortKey(d.Resolved[i]) < findingSortKey(d.Resolved[j]) })
	return d
}

func mapAssets(assets []reportAsset) map[string]reportAsset {
	m := make(map[string]reportAsset, len(assets))
	for _, a := range assets {
		m[a.ID] = a
	}
	return m
}

func mapEdges(edges []reportEdge) map[string]reportEdge {
	m := make(map[string]reportEdge, len(edges))
	for _, e := range edges {
		id := e.ID
		if id == "" {
			id = fmt.Sprintf("%s->%s->%s", e.FromID, e.Type, e.ToID)
		}
		m[id] = e
	}
	return m
}

func mapFindings(r *infrastructureReport) map[string]findingKey {
	m := map[string]findingKey{}
	if r == nil || r.Audit == nil {
		return m
	}
	for _, f := range r.Audit.Findings {
		key := findingKey{Severity: string(f.Severity), Rule: f.Rule, ResourceID: f.ResourceID}
		m[findingSortKey(key)] = key
	}
	return m
}

func findingSortKey(f findingKey) string {
	return strings.Join([]string{f.Severity, f.Rule, f.ResourceID}, "|")
}

func jsonFingerprint(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

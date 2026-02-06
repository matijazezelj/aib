package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/matijazezelj/aib/pkg/models"
)

// GraphData holds a full graph snapshot for export.
type GraphData struct {
	Nodes []models.Node `json:"nodes"`
	Edges []models.Edge `json:"edges"`
}

// ExportJSON returns the graph as a JSON string.
func ExportJSON(ctx context.Context, store Store) (string, error) {
	nodes, err := store.ListNodes(ctx, NodeFilter{})
	if err != nil {
		return "", fmt.Errorf("listing nodes: %w", err)
	}
	edges, err := store.ListEdges(ctx, EdgeFilter{})
	if err != nil {
		return "", fmt.Errorf("listing edges: %w", err)
	}

	data := GraphData{Nodes: nodes, Edges: edges}
	if data.Nodes == nil {
		data.Nodes = []models.Node{}
	}
	if data.Edges == nil {
		data.Edges = []models.Edge{}
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ExportDOT returns the graph in Graphviz DOT format.
func ExportDOT(ctx context.Context, store Store) (string, error) {
	nodes, err := store.ListNodes(ctx, NodeFilter{})
	if err != nil {
		return "", fmt.Errorf("listing nodes: %w", err)
	}
	edges, err := store.ListEdges(ctx, EdgeFilter{})
	if err != nil {
		return "", fmt.Errorf("listing edges: %w", err)
	}

	var b strings.Builder
	b.WriteString("digraph aib {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=filled];\n\n")

	for _, n := range nodes {
		color := nodeColor(n.Type)
		label := fmt.Sprintf("%s\\n(%s)", n.Name, n.Type)
		b.WriteString(fmt.Sprintf("  %q [label=%q, fillcolor=%q];\n", n.ID, label, color))
	}

	b.WriteString("\n")

	for _, e := range edges {
		b.WriteString(fmt.Sprintf("  %q -> %q [label=%q];\n", e.FromID, e.ToID, e.Type))
	}

	b.WriteString("}\n")
	return b.String(), nil
}

// ExportMermaid returns the graph in Mermaid format.
func ExportMermaid(ctx context.Context, store Store) (string, error) {
	nodes, err := store.ListNodes(ctx, NodeFilter{})
	if err != nil {
		return "", fmt.Errorf("listing nodes: %w", err)
	}
	edges, err := store.ListEdges(ctx, EdgeFilter{})
	if err != nil {
		return "", fmt.Errorf("listing edges: %w", err)
	}

	var b strings.Builder
	b.WriteString("graph LR\n")

	for _, n := range nodes {
		safeID := mermaidSafeID(n.ID)
		b.WriteString(fmt.Sprintf("  %s[\"%s (%s)\"]\n", safeID, n.Name, n.Type))
	}

	for _, e := range edges {
		fromID := mermaidSafeID(e.FromID)
		toID := mermaidSafeID(e.ToID)
		b.WriteString(fmt.Sprintf("  %s -->|%s| %s\n", fromID, e.Type, toID))
	}

	return b.String(), nil
}

func nodeColor(t models.AssetType) string {
	switch t {
	case models.AssetVM, models.AssetNode:
		return "#AED6F1"
	case models.AssetPod, models.AssetContainer:
		return "#A3E4D7"
	case models.AssetService:
		return "#F9E79F"
	case models.AssetIngress, models.AssetLoadBalancer:
		return "#F5CBA7"
	case models.AssetDatabase:
		return "#D7BDE2"
	case models.AssetCertificate:
		return "#F1948A"
	case models.AssetSecret:
		return "#E74C3C"
	case models.AssetNetwork, models.AssetSubnet:
		return "#85C1E9"
	case models.AssetDNSRecord:
		return "#82E0AA"
	case models.AssetFirewallRule:
		return "#F0B27A"
	case models.AssetFunction:
		return "#F5B041"
	case models.AssetAPIGateway:
		return "#F9E79F"
	case models.AssetNoSQLDB:
		return "#D7BDE2"
	default:
		return "#D5D8DC"
	}
}

func mermaidSafeID(id string) string {
	r := strings.NewReplacer(":", "_", ".", "_", "-", "_", "/", "_")
	return r.Replace(id)
}

package graph

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/matijazezelj/aib/pkg/models"
)

// CorrelationSummary describes the cross-source identity correlation pass.
type CorrelationSummary struct {
	Groups     int `json:"groups"`
	EdgesAdded int `json:"edges_added"`
}

// CorrelatedAssetGroup is a connected component of assets tied together by
// correlates_with edges.
type CorrelatedAssetGroup struct {
	Key     string        `json:"key"`
	Sources []string      `json:"sources"`
	Nodes   []models.Node `json:"nodes"`
}

// CorrelateIdentities adds deterministic correlates_with edges between nodes
// from different sources that look like the same real-world asset.
//
// This intentionally keeps original source-specific nodes intact. The edge is
// the assertion: "these are probably the same thing", with confidence and key
// metadata for downstream reports/UI. That avoids destructive merging and lets
// users see exactly what AIB inferred.
func CorrelateIdentities(ctx context.Context, store *SQLiteStore) (*CorrelationSummary, error) {
	nodes, err := store.ListNodes(ctx, NodeFilter{})
	if err != nil {
		return nil, err
	}

	existingEdges, err := store.ListEdges(ctx, EdgeFilter{Type: string(models.EdgeCorrelatesWith)})
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool, len(existingEdges))
	for _, edge := range existingEdges {
		existing[edge.ID] = true
	}

	byKey := make(map[string][]models.Node)
	for _, node := range nodes {
		if !correlatableType(node.Type) {
			continue
		}
		for _, key := range correlationKeys(node) {
			byKey[key] = append(byKey[key], node)
		}
	}

	var summary CorrelationSummary
	for key, group := range byKey {
		group = dedupeNodesByID(group)
		if len(group) < 2 || distinctSourceCount(group) < 2 {
			continue
		}
		sort.Slice(group, func(i, j int) bool { return group[i].ID < group[j].ID })
		confidence := confidenceForGroup(group)
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				from, to := group[i], group[j]
				if from.Source == to.Source {
					continue
				}
				edge := models.Edge{
					ID:     GenerateEdgeID(from.ID, to.ID, models.EdgeCorrelatesWith),
					FromID: from.ID,
					ToID:   to.ID,
					Type:   models.EdgeCorrelatesWith,
					Metadata: map[string]string{
						"correlation_key": key,
						"confidence":      confidence,
						"method":          "normalized-name",
					},
				}
				if existing[edge.ID] {
					continue
				}
				if err := store.UpsertEdge(ctx, edge); err != nil {
					return nil, err
				}
				existing[edge.ID] = true
				summary.EdgesAdded++
			}
		}
	}

	groups, err := ListCorrelatedAssetGroups(ctx, store)
	if err != nil {
		return nil, err
	}
	summary.Groups = len(groups)
	return &summary, nil
}

// ListCorrelatedAssetGroups returns connected components formed by
// correlates_with edges, ordered by size and then key.
func ListCorrelatedAssetGroups(ctx context.Context, store *SQLiteStore) ([]CorrelatedAssetGroup, error) {
	edges, err := store.ListEdges(ctx, EdgeFilter{Type: string(models.EdgeCorrelatesWith)})
	if err != nil {
		return nil, err
	}
	if len(edges) == 0 {
		return nil, nil
	}

	allNodes, err := store.ListNodes(ctx, NodeFilter{})
	if err != nil {
		return nil, err
	}
	nodesByID := make(map[string]models.Node, len(allNodes))
	for _, node := range allNodes {
		nodesByID[node.ID] = node
	}

	adj := map[string]map[string]bool{}
	keyVotes := map[string]map[string]int{}
	for _, edge := range edges {
		if adj[edge.FromID] == nil {
			adj[edge.FromID] = map[string]bool{}
		}
		if adj[edge.ToID] == nil {
			adj[edge.ToID] = map[string]bool{}
		}
		adj[edge.FromID][edge.ToID] = true
		adj[edge.ToID][edge.FromID] = true
		key := edge.Metadata["correlation_key"]
		if key != "" {
			for _, id := range []string{edge.FromID, edge.ToID} {
				if keyVotes[id] == nil {
					keyVotes[id] = map[string]int{}
				}
				keyVotes[id][key]++
			}
		}
	}

	visited := map[string]bool{}
	var groups []CorrelatedAssetGroup
	for start := range adj {
		if visited[start] {
			continue
		}
		queue := []string{start}
		visited[start] = true
		var ids []string
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			ids = append(ids, id)
			for next := range adj[id] {
				if !visited[next] {
					visited[next] = true
					queue = append(queue, next)
				}
			}
		}

		var groupNodes []models.Node
		sourceSet := map[string]bool{}
		keyCounts := map[string]int{}
		for _, id := range ids {
			if node, ok := nodesByID[id]; ok {
				groupNodes = append(groupNodes, node)
				sourceSet[node.Source] = true
			}
			for key, count := range keyVotes[id] {
				keyCounts[key] += count
			}
		}
		sort.Slice(groupNodes, func(i, j int) bool { return groupNodes[i].ID < groupNodes[j].ID })
		if len(groupNodes) < 2 {
			continue
		}
		groups = append(groups, CorrelatedAssetGroup{
			Key:     topKey(keyCounts),
			Sources: sortedSet(sourceSet),
			Nodes:   groupNodes,
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Nodes) != len(groups[j].Nodes) {
			return len(groups[i].Nodes) > len(groups[j].Nodes)
		}
		return groups[i].Key < groups[j].Key
	})
	return groups, nil
}

func correlatableType(t models.AssetType) bool {
	switch t {
	case models.AssetVM, models.AssetContainer, models.AssetPod, models.AssetService, models.AssetIngress,
		models.AssetLoadBalancer, models.AssetDatabase, models.AssetBucket, models.AssetDNSRecord,
		models.AssetSecret, models.AssetIPAddress, models.AssetQueue, models.AssetNoSQLDB,
		models.AssetFunction, models.AssetAPIGateway:
		return true
	default:
		return false
	}
}

func correlationKeys(node models.Node) []string {
	candidates := []string{node.Name}
	for _, key := range []string{
		"name", "resource_name", "address", "arn", "hostname", "host", "dns_name",
		"app", "app.kubernetes.io/name", "service", "service_name", "container_name",
		"tag:Name", "tags.Name", "tags.name", "label:app", "label:app.kubernetes.io/name",
	} {
		if value := node.Metadata[key]; value != "" {
			candidates = append(candidates, value)
		}
	}

	seen := map[string]bool{}
	var keys []string
	for _, candidate := range candidates {
		for _, variant := range candidateVariants(candidate) {
			key := normalizeIdentityKey(variant)
			if len(key) < 2 || genericCorrelationKey(key) || seen[key] {
				continue
			}
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

func candidateVariants(s string) []string {
	variants := []string{s}
	if idx := strings.LastIndex(s, "/"); idx >= 0 && idx+1 < len(s) {
		variants = append(variants, s[idx+1:])
	}
	if idx := strings.LastIndex(s, "@"); idx >= 0 && idx+1 < len(s) {
		variants = append(variants, s[idx+1:])
	}
	if idx := strings.LastIndex(s, ":"); idx >= 0 && idx+1 < len(s) {
		variants = append(variants, s[idx+1:])
	}
	return variants
}

func normalizeIdentityKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func genericCorrelationKey(key string) bool {
	switch key {
	case "default", "main", "primary", "secondary", "db", "database", "redis", "postgres", "postgresql", "mysql", "app", "service", "server", "web", "svc", "cluster", "local":
		return true
	default:
		return false
	}
}

func dedupeNodesByID(nodes []models.Node) []models.Node {
	seen := map[string]bool{}
	var out []models.Node
	for _, node := range nodes {
		if seen[node.ID] {
			continue
		}
		seen[node.ID] = true
		out = append(out, node)
	}
	return out
}

func distinctSourceCount(nodes []models.Node) int {
	sources := map[string]bool{}
	for _, node := range nodes {
		sources[node.Source] = true
	}
	return len(sources)
}

func confidenceForGroup(nodes []models.Node) string {
	if len(nodes) >= 3 && distinctSourceCount(nodes) >= 3 {
		return "high"
	}
	return "medium"
}

func sortedSet(set map[string]bool) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func topKey(counts map[string]int) string {
	bestKey := ""
	bestCount := -1
	for key, count := range counts {
		if count > bestCount || (count == bestCount && (bestKey == "" || key < bestKey)) {
			bestKey = key
			bestCount = count
		}
	}
	return bestKey
}

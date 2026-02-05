package graph

import (
	"context"

	"github.com/matijazezelj/aib/pkg/models"
)

// ImpactResult represents the blast radius analysis of a node.
type ImpactResult struct {
	Root          string                `json:"root"`
	AffectedNodes int                   `json:"affected_nodes"`
	ImpactTree    map[string]ImpactNode `json:"impact_tree"`
	AffectedByType map[string]int       `json:"affected_by_type"`
}

// ImpactNode represents a single node in the impact tree.
type ImpactNode struct {
	NodeID       string          `json:"node_id"`
	Node         *models.Node    `json:"node,omitempty"`
	EdgeType     models.EdgeType `json:"edge_type"`
	Depth        int             `json:"depth"`
	PathFromRoot []string        `json:"path_from_root"`
	Children     []ImpactNode    `json:"children,omitempty"`
}

// BlastRadius performs a BFS traversal from the start node to find all affected nodes.
// It traverses in reverse: finds nodes that depend ON the start node (upstream edges),
// since if X fails, everything that depends on X is affected.
func BlastRadius(ctx context.Context, store *SQLiteStore, startNodeID string) (*ImpactResult, error) {
	_, upstream, err := store.BuildAdjacency(ctx)
	if err != nil {
		return nil, err
	}

	visited := make(map[string]bool)
	impactTree := make(map[string]ImpactNode)
	parentMap := make(map[string]string)

	type queueItem struct {
		nodeID string
		depth  int
	}

	queue := []queueItem{{nodeID: startNodeID, depth: 0}}
	visited[startNodeID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// upstream[nodeID] = edges where to_id == nodeID, meaning these are
		// nodes that point TO current (i.e., they depend on current).
		edges := upstream[current.nodeID]
		for _, edge := range edges {
			target := edge.FromID // the node that depends on current
			if visited[target] {
				continue
			}
			visited[target] = true
			parentMap[target] = current.nodeID

			node, _ := store.GetNode(ctx, target)

			path := reconstructPath(parentMap, startNodeID, target)
			impactTree[target] = ImpactNode{
				NodeID:       target,
				Node:         node,
				EdgeType:     edge.Type,
				Depth:        current.depth + 1,
				PathFromRoot: path,
			}

			queue = append(queue, queueItem{nodeID: target, depth: current.depth + 1})
		}
	}

	affectedByType := make(map[string]int)
	for _, impact := range impactTree {
		if impact.Node != nil {
			affectedByType[string(impact.Node.Type)]++
		}
	}

	return &ImpactResult{
		Root:           startNodeID,
		AffectedNodes:  len(impactTree),
		ImpactTree:     impactTree,
		AffectedByType: affectedByType,
	}, nil
}

// BlastRadiusTree returns the impact result as a tree structure rooted at the start node.
// Traverses upstream: finds all nodes that depend on the start node.
func BlastRadiusTree(ctx context.Context, store *SQLiteStore, startNodeID string) (*ImpactNode, error) {
	_, upstream, err := store.BuildAdjacency(ctx)
	if err != nil {
		return nil, err
	}

	rootNode, err := store.GetNode(ctx, startNodeID)
	if err != nil {
		return nil, err
	}

	visited := make(map[string]bool)
	root := &ImpactNode{
		NodeID: startNodeID,
		Node:   rootNode,
		Depth:  0,
	}

	visited[startNodeID] = true
	buildTree(ctx, store, root, upstream, visited, 0)

	return root, nil
}

func buildTree(ctx context.Context, store *SQLiteStore, parent *ImpactNode, upstream map[string][]models.Edge, visited map[string]bool, depth int) {
	// upstream[nodeID] = edges where to_id == nodeID (nodes that point to this one)
	edges := upstream[parent.NodeID]
	for _, edge := range edges {
		target := edge.FromID // the node that depends on parent
		if visited[target] {
			continue
		}
		visited[target] = true

		node, _ := store.GetNode(ctx, target)
		child := ImpactNode{
			NodeID:   target,
			Node:     node,
			EdgeType: edge.Type,
			Depth:    depth + 1,
		}

		buildTree(ctx, store, &child, upstream, visited, depth+1)
		parent.Children = append(parent.Children, child)
	}
}

func reconstructPath(parentMap map[string]string, start, end string) []string {
	path := []string{end}
	current := end
	for current != start {
		parent, ok := parentMap[current]
		if !ok {
			break
		}
		path = append([]string{parent}, path...)
		current = parent
	}
	return path
}

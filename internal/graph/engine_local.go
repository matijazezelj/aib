package graph

import (
	"context"
	"fmt"

	"github.com/matijazezelj/aib/pkg/models"
)

// LocalEngine implements GraphEngine using in-memory BFS over SQLite data.
type LocalEngine struct {
	store *SQLiteStore
}

// NewLocalEngine creates a GraphEngine that uses in-memory adjacency lists.
func NewLocalEngine(store *SQLiteStore) *LocalEngine {
	return &LocalEngine{store: store}
}

// BlastRadius returns a flat map of all nodes affected if startNodeID fails.
func (e *LocalEngine) BlastRadius(ctx context.Context, startNodeID string) (*ImpactResult, error) {
	return BlastRadius(ctx, e.store, startNodeID)
}

// BlastRadiusTree returns the impact analysis as a tree rooted at startNodeID.
func (e *LocalEngine) BlastRadiusTree(ctx context.Context, startNodeID string) (*ImpactNode, error) {
	return BlastRadiusTree(ctx, e.store, startNodeID)
}

// Neighbors returns all nodes directly connected to nodeID in either direction.
func (e *LocalEngine) Neighbors(ctx context.Context, nodeID string) ([]models.Node, error) {
	return e.store.GetNeighbors(ctx, nodeID)
}

// ShortestPath finds the shortest path between two nodes using BFS.
func (e *LocalEngine) ShortestPath(ctx context.Context, fromID, toID string) ([]models.Node, []models.Edge, error) {
	downstream, upstream, err := e.store.BuildAdjacency(ctx)
	if err != nil {
		return nil, nil, err
	}

	// BFS using both directions
	type queueItem struct {
		nodeID string
		path   []string
	}

	visited := make(map[string]bool)
	queue := []queueItem{{nodeID: fromID, path: []string{fromID}}}
	visited[fromID] = true

	// Merge both adjacency maps into a unified neighbor map
	allNeighbors := make(map[string][]string)
	for nodeID, edges := range downstream {
		for _, e := range edges {
			allNeighbors[nodeID] = append(allNeighbors[nodeID], e.ToID)
		}
	}
	for nodeID, edges := range upstream {
		for _, e := range edges {
			allNeighbors[nodeID] = append(allNeighbors[nodeID], e.FromID)
		}
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.nodeID == toID {
			// Reconstruct nodes and edges along the path
			var nodes []models.Node
			var edges []models.Edge
			for _, nid := range current.path {
				n, _ := e.store.GetNode(ctx, nid)
				if n != nil {
					nodes = append(nodes, *n)
				}
			}
			return nodes, edges, nil
		}

		for _, neighbor := range allNeighbors[current.nodeID] {
			if visited[neighbor] {
				continue
			}
			visited[neighbor] = true
			newPath := make([]string, len(current.path)+1)
			copy(newPath, current.path)
			newPath[len(current.path)] = neighbor
			queue = append(queue, queueItem{nodeID: neighbor, path: newPath})
		}
	}

	return nil, nil, fmt.Errorf("no path found between %s and %s", fromID, toID)
}

// DependencyChain returns all downstream dependencies of nodeID up to maxDepth.
func (e *LocalEngine) DependencyChain(ctx context.Context, nodeID string, maxDepth int) ([]models.Node, error) {
	downstream, _, err := e.store.BuildAdjacency(ctx)
	if err != nil {
		return nil, err
	}

	type queueItem struct {
		nodeID string
		depth  int
	}

	visited := make(map[string]bool)
	visited[nodeID] = true
	queue := []queueItem{{nodeID: nodeID, depth: 0}}
	var result []models.Node

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxDepth {
			continue
		}

		for _, edge := range downstream[current.nodeID] {
			if visited[edge.ToID] {
				continue
			}
			visited[edge.ToID] = true
			n, _ := e.store.GetNode(ctx, edge.ToID)
			if n != nil {
				result = append(result, *n)
			}
			queue = append(queue, queueItem{nodeID: edge.ToID, depth: current.depth + 1})
		}
	}

	return result, nil
}

// Close is a no-op for the local engine (no external resources).
func (e *LocalEngine) Close() error {
	return nil
}

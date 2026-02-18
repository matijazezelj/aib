package graph

import (
	"context"
	"fmt"
	"sort"

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

	// Build a reverse lookup: for any pair of neighbors, find the edge between them.
	allEdgesMap := make(map[string]models.Edge) // "from->to" → edge
	for _, edgeList := range downstream {
		for _, edge := range edgeList {
			allEdgesMap[edge.FromID+"->"+edge.ToID] = edge
		}
	}
	for _, edgeList := range upstream {
		for _, edge := range edgeList {
			allEdgesMap[edge.FromID+"->"+edge.ToID] = edge
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
			// Reconstruct edges between consecutive path nodes
			for i := 0; i+1 < len(current.path); i++ {
				a, b := current.path[i], current.path[i+1]
				if edge, ok := allEdgesMap[a+"->"+b]; ok {
					edges = append(edges, edge)
				} else if edge, ok := allEdgesMap[b+"->"+a]; ok {
					edges = append(edges, edge)
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

// FindCycles detects circular dependencies using DFS with a recursion stack.
func (e *LocalEngine) FindCycles(ctx context.Context) ([][]string, error) {
	downstream, _, err := e.store.BuildAdjacency(ctx)
	if err != nil {
		return nil, err
	}

	// Collect all node IDs that appear in any edge.
	nodeSet := make(map[string]bool)
	for from, edges := range downstream {
		nodeSet[from] = true
		for _, edge := range edges {
			nodeSet[edge.ToID] = true
		}
	}

	visited := make(map[string]bool)
	onStack := make(map[string]bool)
	var cycles [][]string
	seen := make(map[string]bool) // dedup normalized cycle keys

	var dfs func(nodeID string, path []string)
	dfs = func(nodeID string, path []string) {
		visited[nodeID] = true
		onStack[nodeID] = true

		for _, edge := range downstream[nodeID] {
			next := edge.ToID
			if onStack[next] {
				// Found a cycle — extract it from path.
				var cycle []string
				found := false
				for _, p := range path {
					if p == next {
						found = true
					}
					if found {
						cycle = append(cycle, p)
					}
				}
				if len(cycle) > 0 {
					normalized := normalizeCycle(cycle)
					key := fmt.Sprintf("%v", normalized)
					if !seen[key] {
						seen[key] = true
						cycles = append(cycles, normalized)
					}
				}
			} else if !visited[next] {
				dfs(next, append(path, next))
			}
		}

		onStack[nodeID] = false
	}

	for nodeID := range nodeSet {
		if !visited[nodeID] {
			dfs(nodeID, []string{nodeID})
		}
	}

	return cycles, nil
}

// normalizeCycle rotates a cycle so it starts with the smallest ID.
func normalizeCycle(cycle []string) []string {
	if len(cycle) == 0 {
		return cycle
	}
	minIdx := 0
	for i, id := range cycle {
		if id < cycle[minIdx] {
			minIdx = i
		}
	}
	result := make([]string, len(cycle))
	for i := range cycle {
		result[i] = cycle[(minIdx+i)%len(cycle)]
	}
	return result
}

// FindSPOF identifies single points of failure by computing blast radius for each node.
func (e *LocalEngine) FindSPOF(ctx context.Context, minAffected int) ([]SPOFNode, error) {
	nodes, err := e.store.ListNodes(ctx, NodeFilter{})
	if err != nil {
		return nil, err
	}

	var results []SPOFNode
	for _, n := range nodes {
		result, err := e.BlastRadius(ctx, n.ID)
		if err != nil {
			continue
		}
		if result.AffectedNodes >= minAffected {
			nodeCopy := n
			results = append(results, SPOFNode{
				Node:           &nodeCopy,
				AffectedCount:  result.AffectedNodes,
				AffectedByType: result.AffectedByType,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].AffectedCount > results[j].AffectedCount
	})

	return results, nil
}

// FindOrphans returns nodes with no edges.
func (e *LocalEngine) FindOrphans(ctx context.Context) ([]models.Node, error) {
	return e.store.FindOrphanNodes(ctx)
}

// Close is a no-op for the local engine (no external resources).
func (e *LocalEngine) Close() error {
	return nil
}

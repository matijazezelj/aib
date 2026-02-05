package graph

import (
	"context"

	"github.com/matijazezelj/aib/pkg/models"
)

// GraphEngine abstracts graph traversal operations.
// Implementations may use in-memory BFS (LocalEngine) or
// a native graph database like Memgraph (MemgraphEngine).
type GraphEngine interface {
	// BlastRadius returns a flat map of all nodes affected if startNodeID fails.
	BlastRadius(ctx context.Context, startNodeID string) (*ImpactResult, error)

	// BlastRadiusTree returns the same analysis as a tree rooted at startNodeID.
	BlastRadiusTree(ctx context.Context, startNodeID string) (*ImpactNode, error)

	// Neighbors returns all nodes directly connected to nodeID (both directions).
	Neighbors(ctx context.Context, nodeID string) ([]models.Node, error)

	// ShortestPath returns the shortest path between two nodes, if one exists.
	ShortestPath(ctx context.Context, fromID, toID string) ([]models.Node, []models.Edge, error)

	// DependencyChain returns all nodes reachable downstream from nodeID
	// (what does nodeID depend on, transitively).
	DependencyChain(ctx context.Context, nodeID string, maxDepth int) ([]models.Node, error)

	// Close releases any resources held by the engine.
	Close() error
}

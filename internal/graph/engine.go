package graph

import (
	"context"

	"github.com/matijazezelj/aib/pkg/models"
)

// SPOFNode represents a single point of failure in the graph.
type SPOFNode struct {
	Node           *models.Node   `json:"node"`
	AffectedCount  int            `json:"affected_count"`
	AffectedByType map[string]int `json:"affected_by_type"`
}

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

	// FindCycles detects circular dependencies in the graph.
	// Returns a slice of cycles, where each cycle is a slice of node IDs.
	FindCycles(ctx context.Context) ([][]string, error)

	// FindSPOF identifies single points of failure — nodes whose removal
	// would affect at least minAffected other nodes.
	FindSPOF(ctx context.Context, minAffected int) ([]SPOFNode, error)

	// FindOrphans returns nodes that have no edges (neither incoming nor outgoing).
	FindOrphans(ctx context.Context) ([]models.Node, error)

	// Close releases any resources held by the engine.
	Close() error
}

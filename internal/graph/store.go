package graph

import (
	"context"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
)

// Store defines the interface for persisting and querying the asset graph.
type Store interface {
	// Init initializes the store (creates tables, indexes, etc.).
	Init(ctx context.Context) error

	// Close closes the store connection.
	Close() error

	// UpsertNode inserts or updates a node.
	UpsertNode(ctx context.Context, node models.Node) error

	// UpsertEdge inserts or updates an edge.
	UpsertEdge(ctx context.Context, edge models.Edge) error

	// GetNode retrieves a node by ID.
	GetNode(ctx context.Context, id string) (*models.Node, error)

	// ListNodes returns nodes matching the given filters.
	ListNodes(ctx context.Context, filter NodeFilter) ([]models.Node, error)

	// ListEdges returns edges matching the given filters.
	ListEdges(ctx context.Context, filter EdgeFilter) ([]models.Edge, error)

	// GetNeighbors returns nodes directly connected to the given node.
	GetNeighbors(ctx context.Context, nodeID string) ([]models.Node, error)

	// GetEdgesFrom returns edges originating from the given node.
	GetEdgesFrom(ctx context.Context, nodeID string) ([]models.Edge, error)

	// GetEdgesTo returns edges pointing to the given node.
	GetEdgesTo(ctx context.Context, nodeID string) ([]models.Edge, error)

	// DeleteNode removes a node and its connected edges.
	DeleteNode(ctx context.Context, id string) error

	// NodeCount returns the total number of nodes.
	NodeCount(ctx context.Context) (int, error)

	// EdgeCount returns the total number of edges.
	EdgeCount(ctx context.Context) (int, error)

	// RecordScan records a scan operation.
	RecordScan(ctx context.Context, scan Scan) (int64, error)

	// UpdateScan updates a scan record.
	UpdateScan(ctx context.Context, id int64, status string, nodesFound, edgesFound int) error

	// ListScans returns recent scan records.
	ListScans(ctx context.Context, limit int) ([]Scan, error)
}

// NodeFilter specifies criteria for listing nodes.
type NodeFilter struct {
	Type     string
	Source   string
	Provider string
}

// EdgeFilter specifies criteria for listing edges.
type EdgeFilter struct {
	Type   string
	FromID string
	ToID   string
}

// Scan represents a scan operation record.
type Scan struct {
	ID         int64      `json:"id"`
	Source     string     `json:"source"`
	SourcePath string     `json:"source_path"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	NodesFound int        `json:"nodes_found"`
	EdgesFound int        `json:"edges_found"`
	Status     string     `json:"status"`
}

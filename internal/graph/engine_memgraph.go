package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// MemgraphEngine implements GraphEngine using Memgraph via the Bolt protocol.
type MemgraphEngine struct {
	driver     neo4j.DriverWithContext
	newSession sessionFactory
	fallback   *LocalEngine
	logger     *slog.Logger
}

// NewMemgraphEngine creates a GraphEngine backed by Memgraph.
// Falls back to the provided LocalEngine on query failures.
func NewMemgraphEngine(uri, username, password string, fallback *LocalEngine, logger *slog.Logger) (*MemgraphEngine, error) {
	auth := neo4j.NoAuth()
	if username != "" {
		auth = neo4j.BasicAuth(username, password, "")
	}

	driver, err := neo4j.NewDriverWithContext(uri, auth)
	if err != nil {
		return nil, fmt.Errorf("creating memgraph driver: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(context.Background())
		return nil, fmt.Errorf("memgraph connectivity check failed: %w", err)
	}

	logger.Info("memgraph engine initialized", "uri", uri)
	return &MemgraphEngine{
		driver:     driver,
		newSession: newNeo4jSessionFactory(driver),
		fallback:   fallback,
		logger:     logger,
	}, nil
}

// Driver returns the underlying neo4j driver for use by SyncedStore.
func (e *MemgraphEngine) Driver() neo4j.DriverWithContext {
	return e.driver
}

// Close closes the Memgraph driver connection.
func (e *MemgraphEngine) Close() error {
	return e.driver.Close(context.Background())
}

// BlastRadius returns all nodes affected if startNodeID fails, using Cypher traversal.
func (e *MemgraphEngine) BlastRadius(ctx context.Context, startNodeID string) (*ImpactResult, error) {
	session := e.newSession(ctx)
	defer session.Close(ctx) //nolint:errcheck // best-effort cleanup

	// Find all nodes that transitively point to the start node (upstream traversal).
	// Edge direction: (from)-[:EDGE]->(to) means "from depends on to".
	// If startNode fails, affected = all nodes with a path TO startNode.
	cypher := `
		MATCH (affected:Asset)-[*1..]->(root:Asset {id: $startID})
		WHERE affected.id <> $startID
		WITH DISTINCT affected
		RETURN affected.id AS id,
		       affected.name AS name,
		       affected.type AS type,
		       affected.source AS source,
		       affected.source_file AS source_file,
		       affected.provider AS provider,
		       affected.metadata AS metadata,
		       affected.expires_at AS expires_at,
		       affected.last_seen AS last_seen,
		       affected.first_seen AS first_seen
		ORDER BY type, name
	`

	result, err := session.Run(ctx, cypher, map[string]any{"startID": startNodeID})
	if err != nil {
		e.logger.Warn("memgraph blast radius failed, falling back", "error", err)
		return e.fallback.BlastRadius(ctx, startNodeID)
	}

	impactTree := make(map[string]ImpactNode)
	for result.Next(ctx) {
		node := recordToNode(result.Record())
		impactTree[node.ID] = ImpactNode{
			NodeID: node.ID,
			Node:   node,
		}
	}

	if err := result.Err(); err != nil {
		e.logger.Warn("memgraph result error, falling back", "error", err)
		return e.fallback.BlastRadius(ctx, startNodeID)
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

// BlastRadiusTree returns the impact analysis as a tree, using Cypher traversal.
func (e *MemgraphEngine) BlastRadiusTree(ctx context.Context, startNodeID string) (*ImpactNode, error) {
	// Fetch the root node and all upstream edges in the affected subgraph,
	// then reconstruct the tree in Go (same structure as LocalEngine).
	session := e.newSession(ctx)
	defer session.Close(ctx) //nolint:errcheck // best-effort cleanup

	// Get root node
	rootResult, err := session.Run(ctx, `
		MATCH (n:Asset {id: $id})
		RETURN n.id AS id, n.name AS name, n.type AS type, n.source AS source,
		       n.source_file AS source_file, n.provider AS provider,
		       n.metadata AS metadata, n.expires_at AS expires_at,
		       n.last_seen AS last_seen, n.first_seen AS first_seen
	`, map[string]any{"id": startNodeID})
	if err != nil {
		e.logger.Warn("memgraph tree root query failed, falling back", "error", err)
		return e.fallback.BlastRadiusTree(ctx, startNodeID)
	}

	var rootNode *models.Node
	if rootResult.Next(ctx) {
		rootNode = recordToNode(rootResult.Record())
	}

	// Fetch all affected nodes (upstream traversal)
	nodeMap := make(map[string]*models.Node)
	if rootNode != nil {
		nodeMap[rootNode.ID] = rootNode
	}

	nodesResult, err := session.Run(ctx, `
		MATCH (affected:Asset)-[*1..]->(root:Asset {id: $startID})
		WITH DISTINCT affected
		RETURN affected.id AS id, affected.name AS name, affected.type AS type,
		       affected.source AS source, affected.source_file AS source_file,
		       affected.provider AS provider, affected.metadata AS metadata,
		       affected.expires_at AS expires_at, affected.last_seen AS last_seen,
		       affected.first_seen AS first_seen
	`, map[string]any{"startID": startNodeID})
	if err != nil {
		e.logger.Warn("memgraph affected nodes query failed, falling back", "error", err)
		return e.fallback.BlastRadiusTree(ctx, startNodeID)
	}

	var affectedIDs []string
	for nodesResult.Next(ctx) {
		n := recordToNode(nodesResult.Record())
		nodeMap[n.ID] = n
		affectedIDs = append(affectedIDs, n.ID)
	}

	// Collect all node IDs in the subgraph (affected + root)
	allIDs := append(affectedIDs, startNodeID)

	// Fetch all edges between nodes in the affected subgraph
	edgeResult, err := session.Run(ctx, `
		MATCH (a:Asset)-[r:EDGE]->(b:Asset)
		WHERE a.id IN $ids AND b.id IN $ids
		RETURN a.id AS from_id, r.type AS edge_type, b.id AS to_id
	`, map[string]any{"ids": allIDs})
	if err != nil {
		e.logger.Warn("memgraph tree edge query failed, falling back", "error", err)
		return e.fallback.BlastRadiusTree(ctx, startNodeID)
	}

	// Build upstream adjacency: map[to_id] → list of (from_id, edge_type)
	upstream := make(map[string][]mgEdgeInfo)
	for edgeResult.Next(ctx) {
		rec := edgeResult.Record()
		fromID, _ := rec.Get("from_id")
		toID, _ := rec.Get("to_id")
		edgeType, _ := rec.Get("edge_type")
		if fromID != nil && toID != nil {
			upstream[toID.(string)] = append(upstream[toID.(string)], mgEdgeInfo{
				fromID:   fromID.(string),
				edgeType: models.EdgeType(toString(edgeType)),
			})
		}
	}

	if err := edgeResult.Err(); err != nil {
		e.logger.Warn("memgraph edge result error, falling back", "error", err)
		return e.fallback.BlastRadiusTree(ctx, startNodeID)
	}

	// Build tree using the upstream edges
	root := &ImpactNode{
		NodeID: startNodeID,
		Node:   rootNode,
		Depth:  0,
	}

	visited := map[string]bool{startNodeID: true}
	buildMgTree(root, upstream, nodeMap, visited, 0)

	return root, nil
}

func buildMgTree(parent *ImpactNode, upstream map[string][]mgEdgeInfo, nodeMap map[string]*models.Node, visited map[string]bool, depth int) {
	for _, ei := range upstream[parent.NodeID] {
		if visited[ei.fromID] {
			continue
		}
		visited[ei.fromID] = true

		child := ImpactNode{
			NodeID:   ei.fromID,
			Node:     nodeMap[ei.fromID],
			EdgeType: ei.edgeType,
			Depth:    depth + 1,
		}
		buildMgTree(&child, upstream, nodeMap, visited, depth+1)
		parent.Children = append(parent.Children, child)
	}
}

// mgEdgeInfo holds an edge's source node and type during tree reconstruction.
type mgEdgeInfo struct {
	fromID   string
	edgeType models.EdgeType
}

// Neighbors returns all nodes connected to nodeID in either direction.
func (e *MemgraphEngine) Neighbors(ctx context.Context, nodeID string) ([]models.Node, error) {
	session := e.newSession(ctx)
	defer session.Close(ctx) //nolint:errcheck // best-effort cleanup

	cypher := `
		MATCH (n:Asset {id: $id})-[r:EDGE]-(neighbor:Asset)
		RETURN DISTINCT neighbor.id AS id, neighbor.name AS name,
		       neighbor.type AS type, neighbor.source AS source,
		       neighbor.source_file AS source_file, neighbor.provider AS provider,
		       neighbor.metadata AS metadata, neighbor.expires_at AS expires_at,
		       neighbor.last_seen AS last_seen, neighbor.first_seen AS first_seen
		ORDER BY type, name
	`

	result, err := session.Run(ctx, cypher, map[string]any{"id": nodeID})
	if err != nil {
		e.logger.Warn("memgraph neighbors failed, falling back", "error", err)
		return e.fallback.Neighbors(ctx, nodeID)
	}

	var nodes []models.Node
	for result.Next(ctx) {
		n := recordToNode(result.Record())
		nodes = append(nodes, *n)
	}

	if err := result.Err(); err != nil {
		e.logger.Warn("memgraph neighbors result error, falling back", "error", err)
		return e.fallback.Neighbors(ctx, nodeID)
	}

	return nodes, nil
}

// ShortestPath finds the shortest path between two nodes using Cypher shortestPath.
func (e *MemgraphEngine) ShortestPath(ctx context.Context, fromID, toID string) ([]models.Node, []models.Edge, error) {
	session := e.newSession(ctx)
	defer session.Close(ctx) //nolint:errcheck // best-effort cleanup

	cypher := `
		MATCH p = shortestPath((a:Asset {id: $fromID})-[*]-(b:Asset {id: $toID}))
		UNWIND nodes(p) AS n
		RETURN n.id AS id, n.name AS name, n.type AS type,
		       n.source AS source, n.source_file AS source_file,
		       n.provider AS provider, n.metadata AS metadata,
		       n.expires_at AS expires_at, n.last_seen AS last_seen,
		       n.first_seen AS first_seen
	`

	result, err := session.Run(ctx, cypher, map[string]any{"fromID": fromID, "toID": toID})
	if err != nil {
		e.logger.Warn("memgraph shortest path failed, falling back", "error", err)
		return e.fallback.ShortestPath(ctx, fromID, toID)
	}

	var nodes []models.Node
	for result.Next(ctx) {
		n := recordToNode(result.Record())
		nodes = append(nodes, *n)
	}

	if err := result.Err(); err != nil {
		e.logger.Warn("memgraph shortest path result error, falling back", "error", err)
		return e.fallback.ShortestPath(ctx, fromID, toID)
	}

	if len(nodes) == 0 {
		return nil, nil, fmt.Errorf("no path found between %s and %s", fromID, toID)
	}

	return nodes, nil, nil
}

// DependencyChain returns all downstream dependencies up to maxDepth using Cypher.
func (e *MemgraphEngine) DependencyChain(ctx context.Context, nodeID string, maxDepth int) ([]models.Node, error) {
	if maxDepth <= 0 || maxDepth > 50 {
		maxDepth = 50
	}

	session := e.newSession(ctx)
	defer session.Close(ctx) //nolint:errcheck // best-effort cleanup

	cypher := fmt.Sprintf(`
		MATCH (start:Asset {id: $id})-[*1..%d]->(dep:Asset)
		RETURN DISTINCT dep.id AS id, dep.name AS name, dep.type AS type,
		       dep.source AS source, dep.source_file AS source_file,
		       dep.provider AS provider, dep.metadata AS metadata,
		       dep.expires_at AS expires_at, dep.last_seen AS last_seen,
		       dep.first_seen AS first_seen
		ORDER BY type, name
	`, maxDepth)

	result, err := session.Run(ctx, cypher, map[string]any{"id": nodeID})
	if err != nil {
		e.logger.Warn("memgraph dependency chain failed, falling back", "error", err)
		return e.fallback.DependencyChain(ctx, nodeID, maxDepth)
	}

	var nodes []models.Node
	for result.Next(ctx) {
		n := recordToNode(result.Record())
		nodes = append(nodes, *n)
	}

	if err := result.Err(); err != nil {
		e.logger.Warn("memgraph dependency chain result error, falling back", "error", err)
		return e.fallback.DependencyChain(ctx, nodeID, maxDepth)
	}

	return nodes, nil
}

// recordToNode converts a neo4j record to a models.Node.
func recordToNode(record *neo4j.Record) *models.Node {
	node := &models.Node{
		ID:         getRecordString(record, "id"),
		Name:       getRecordString(record, "name"),
		Type:       models.AssetType(getRecordString(record, "type")),
		Source:     getRecordString(record, "source"),
		SourceFile: getRecordString(record, "source_file"),
		Provider:   getRecordString(record, "provider"),
		Metadata:   make(map[string]string),
	}

	if metaStr := getRecordString(record, "metadata"); metaStr != "" {
		_ = json.Unmarshal([]byte(metaStr), &node.Metadata)
	}

	if ea := getRecordString(record, "expires_at"); ea != "" {
		t, err := time.Parse(time.RFC3339, ea)
		if err == nil {
			node.ExpiresAt = &t
		}
	}
	if ls := getRecordString(record, "last_seen"); ls != "" {
		node.LastSeen, _ = time.Parse(time.RFC3339, ls)
	}
	if fs := getRecordString(record, "first_seen"); fs != "" {
		node.FirstSeen, _ = time.Parse(time.RFC3339, fs)
	}

	return node
}

func getRecordString(record *neo4j.Record, key string) string {
	v, ok := record.Get(key)
	if !ok || v == nil {
		return ""
	}
	return toString(v)
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

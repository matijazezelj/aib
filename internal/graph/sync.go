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

// SyncToMemgraph performs a full synchronization from SQLite to Memgraph.
// It clears all Memgraph data and re-inserts everything from SQLite.
func SyncToMemgraph(ctx context.Context, store *SQLiteStore, driver neo4j.DriverWithContext, logger *slog.Logger) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx) //nolint:errcheck // best-effort cleanup

	// Step 1: Clear Memgraph
	logger.Info("clearing memgraph data")
	_, err := session.Run(ctx, "MATCH (n) DETACH DELETE n", nil)
	if err != nil {
		return fmt.Errorf("clearing memgraph: %w", err)
	}

	// Step 2: Create index
	logger.Info("creating memgraph indexes")
	for _, cypher := range []string{
		"CREATE INDEX ON :Asset(id)",
		"CREATE INDEX ON :Asset(type)",
		"CREATE INDEX ON :Asset(source)",
	} {
		_, err := session.Run(ctx, cypher, nil)
		if err != nil {
			logger.Warn("creating index (may already exist)", "error", err)
		}
	}

	// Step 3: Load all nodes from SQLite
	nodes, err := store.ListNodes(ctx, NodeFilter{})
	if err != nil {
		return fmt.Errorf("listing nodes from sqlite: %w", err)
	}

	logger.Info("syncing nodes to memgraph", "count", len(nodes))

	batchSize := 500
	for i := 0; i < len(nodes); i += batchSize {
		end := i + batchSize
		if end > len(nodes) {
			end = len(nodes)
		}
		batch := nodes[i:end]

		nodeParams := make([]map[string]any, len(batch))
		for j, n := range batch {
			nodeParams[j] = nodeToParams(n)
		}

		cypher := `
			UNWIND $nodes AS n
			CREATE (a:Asset {
				id: n.id, name: n.name, type: n.type,
				source: n.source, source_file: n.sourceFile,
				provider: n.provider, metadata: n.metadata,
				expires_at: n.expiresAt, last_seen: n.lastSeen,
				first_seen: n.firstSeen
			})
		`
		_, err := session.Run(ctx, cypher, map[string]any{"nodes": nodeParams})
		if err != nil {
			return fmt.Errorf("syncing node batch %d-%d: %w", i, end, err)
		}
	}

	// Step 4: Load all edges from SQLite
	edges, err := store.AllEdges(ctx)
	if err != nil {
		return fmt.Errorf("listing edges from sqlite: %w", err)
	}

	logger.Info("syncing edges to memgraph", "count", len(edges))

	for i := 0; i < len(edges); i += batchSize {
		end := i + batchSize
		if end > len(edges) {
			end = len(edges)
		}
		batch := edges[i:end]

		edgeParams := make([]map[string]any, len(batch))
		for j, e := range batch {
			edgeParams[j] = edgeToParams(e)
		}

		cypher := `
			UNWIND $edges AS e
			MATCH (from:Asset {id: e.fromID})
			MATCH (to:Asset {id: e.toID})
			CREATE (from)-[:EDGE {id: e.id, type: e.type, metadata: e.metadata}]->(to)
		`
		_, err := session.Run(ctx, cypher, map[string]any{"edges": edgeParams})
		if err != nil {
			return fmt.Errorf("syncing edge batch %d-%d: %w", i, end, err)
		}
	}

	logger.Info("memgraph sync complete", "nodes", len(nodes), "edges", len(edges))
	fmt.Printf("Synced %d nodes and %d edges to Memgraph\n", len(nodes), len(edges))
	return nil
}

func nodeToParams(n models.Node) map[string]any {
	meta, _ := json.Marshal(n.Metadata)
	var expiresAt any
	if n.ExpiresAt != nil {
		expiresAt = n.ExpiresAt.Format(time.RFC3339)
	}
	return map[string]any{
		"id":         n.ID,
		"name":       n.Name,
		"type":       string(n.Type),
		"source":     n.Source,
		"sourceFile": n.SourceFile,
		"provider":   n.Provider,
		"metadata":   string(meta),
		"expiresAt":  expiresAt,
		"lastSeen":   n.LastSeen.Format(time.RFC3339),
		"firstSeen":  n.FirstSeen.Format(time.RFC3339),
	}
}

func edgeToParams(e models.Edge) map[string]any {
	meta, _ := json.Marshal(e.Metadata)
	return map[string]any{
		"id":       e.ID,
		"fromID":   e.FromID,
		"toID":     e.ToID,
		"type":     string(e.Type),
		"metadata": string(meta),
	}
}

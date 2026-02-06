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

// SyncedStore wraps a SQLiteStore and mirrors write operations to Memgraph.
// Memgraph failures are logged but never block the SQLite write.
type SyncedStore struct {
	*SQLiteStore
	driver neo4j.DriverWithContext
	logger *slog.Logger
}

// NewSyncedStore creates a SyncedStore. If driver is nil, no syncing occurs.
func NewSyncedStore(store *SQLiteStore, driver neo4j.DriverWithContext, logger *slog.Logger) *SyncedStore {
	return &SyncedStore{
		SQLiteStore: store,
		driver:      driver,
		logger:      logger,
	}
}

// UpsertNode inserts or updates a node in SQLite and mirrors the write to Memgraph.
func (s *SyncedStore) UpsertNode(ctx context.Context, node models.Node) error {
	if err := s.SQLiteStore.UpsertNode(ctx, node); err != nil {
		return err
	}
	if s.driver != nil {
		if err := s.syncNode(ctx, node); err != nil {
			s.logger.Warn("failed to sync node to memgraph", "nodeID", node.ID, "error", err)
		}
	}
	return nil
}

// UpsertEdge inserts or updates an edge in SQLite and mirrors the write to Memgraph.
func (s *SyncedStore) UpsertEdge(ctx context.Context, edge models.Edge) error {
	if err := s.SQLiteStore.UpsertEdge(ctx, edge); err != nil {
		return err
	}
	if s.driver != nil {
		if err := s.syncEdge(ctx, edge); err != nil {
			s.logger.Warn("failed to sync edge to memgraph", "edgeID", edge.ID, "error", err)
		}
	}
	return nil
}

// DeleteNode removes a node from SQLite and Memgraph.
func (s *SyncedStore) DeleteNode(ctx context.Context, id string) error {
	if err := s.SQLiteStore.DeleteNode(ctx, id); err != nil {
		return err
	}
	if s.driver != nil {
		if err := s.deleteMemgraphNode(ctx, id); err != nil {
			s.logger.Warn("failed to delete node from memgraph", "nodeID", id, "error", err)
		}
	}
	return nil
}

// Close closes both the SQLite and Memgraph connections.
func (s *SyncedStore) Close() error {
	sqlErr := s.SQLiteStore.Close()
	if s.driver != nil {
		if mgErr := s.driver.Close(context.Background()); mgErr != nil && sqlErr == nil {
			return mgErr
		}
	}
	return sqlErr
}

func (s *SyncedStore) syncNode(ctx context.Context, node models.Node) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	meta, _ := json.Marshal(node.Metadata)
	var expiresAt any
	if node.ExpiresAt != nil {
		expiresAt = node.ExpiresAt.Format(time.RFC3339)
	}

	cypher := `
		MERGE (n:Asset {id: $id})
		SET n.name = $name,
		    n.type = $type,
		    n.source = $source,
		    n.source_file = $sourceFile,
		    n.provider = $provider,
		    n.metadata = $metadata,
		    n.expires_at = $expiresAt,
		    n.last_seen = $lastSeen,
		    n.first_seen = $firstSeen
	`

	_, err := session.Run(ctx, cypher, map[string]any{
		"id":         node.ID,
		"name":       node.Name,
		"type":       string(node.Type),
		"source":     node.Source,
		"sourceFile": node.SourceFile,
		"provider":   node.Provider,
		"metadata":   string(meta),
		"expiresAt":  expiresAt,
		"lastSeen":   node.LastSeen.Format(time.RFC3339),
		"firstSeen":  node.FirstSeen.Format(time.RFC3339),
	})
	return err
}

func (s *SyncedStore) syncEdge(ctx context.Context, edge models.Edge) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	meta, _ := json.Marshal(edge.Metadata)

	cypher := `
		MATCH (from:Asset {id: $fromID})
		MATCH (to:Asset {id: $toID})
		MERGE (from)-[r:EDGE {id: $id}]->(to)
		SET r.type = $type,
		    r.metadata = $metadata
	`

	_, err := session.Run(ctx, cypher, map[string]any{
		"id":       edge.ID,
		"fromID":   edge.FromID,
		"toID":     edge.ToID,
		"type":     string(edge.Type),
		"metadata": string(meta),
	})
	return err
}

func (s *SyncedStore) deleteMemgraphNode(ctx context.Context, id string) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	_, err := session.Run(ctx, `MATCH (n:Asset {id: $id}) DETACH DELETE n`, map[string]any{"id": id})
	return err
}

// Underlying returns the wrapped SQLiteStore.
func (s *SyncedStore) Underlying() *SQLiteStore {
	return s.SQLiteStore
}

// HasMemgraph returns true if Memgraph syncing is active.
func (s *SyncedStore) HasMemgraph() bool {
	return s.driver != nil
}

// MemgraphDriver returns the Memgraph driver, or nil.
func (s *SyncedStore) MemgraphDriver() neo4j.DriverWithContext {
	return s.driver
}

func init() {
	// Ensure SyncedStore still satisfies all the methods consumers need.
	// Since it embeds *SQLiteStore, all SQLiteStore methods are promoted.
	_ = fmt.Sprintf // use fmt
}

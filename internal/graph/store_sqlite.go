package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,
    source      TEXT NOT NULL,
    source_file TEXT,
    provider    TEXT,
    metadata    TEXT,
    expires_at  DATETIME,
    last_seen   DATETIME NOT NULL,
    first_seen  DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS edges (
    id        TEXT PRIMARY KEY,
    from_id   TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    to_id     TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    type      TEXT NOT NULL,
    metadata  TEXT,
    UNIQUE(from_id, to_id, type)
);

CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type);
CREATE INDEX IF NOT EXISTS idx_nodes_source ON nodes(source);
CREATE INDEX IF NOT EXISTS idx_nodes_expires_at ON nodes(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_id);
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(type);

CREATE TABLE IF NOT EXISTS scans (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source      TEXT NOT NULL,
    source_path TEXT NOT NULL,
    started_at  DATETIME NOT NULL,
    finished_at DATETIME,
    nodes_found INTEGER DEFAULT 0,
    edges_found INTEGER DEFAULT 0,
    status      TEXT DEFAULT 'running'
);
`

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite-backed store.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(wal)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Init creates the database schema if it doesn't exist.
func (s *SQLiteStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// UpsertNode inserts or updates a node in the store.
func (s *SQLiteStore) UpsertNode(ctx context.Context, node models.Node) error {
	meta, err := json.Marshal(node.Metadata)
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	var expiresAt *string
	if node.ExpiresAt != nil {
		t := node.ExpiresAt.Format(time.RFC3339)
		expiresAt = &t
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO nodes (id, name, type, source, source_file, provider, metadata, expires_at, last_seen, first_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			type = excluded.type,
			source = excluded.source,
			source_file = excluded.source_file,
			provider = excluded.provider,
			metadata = excluded.metadata,
			expires_at = excluded.expires_at,
			last_seen = excluded.last_seen
	`, node.ID, node.Name, string(node.Type), node.Source, node.SourceFile,
		node.Provider, string(meta), expiresAt,
		node.LastSeen.Format(time.RFC3339), node.FirstSeen.Format(time.RFC3339))
	return err
}

// UpsertEdge inserts or updates an edge in the store.
func (s *SQLiteStore) UpsertEdge(ctx context.Context, edge models.Edge) error {
	meta, err := json.Marshal(edge.Metadata)
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO edges (id, from_id, to_id, type, metadata)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(from_id, to_id, type) DO UPDATE SET
			metadata = excluded.metadata
	`, edge.ID, edge.FromID, edge.ToID, string(edge.Type), string(meta))
	return err
}

// GetNode retrieves a single node by ID.
func (s *SQLiteStore) GetNode(ctx context.Context, id string) (*models.Node, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, type, source, source_file, provider, metadata, expires_at, last_seen, first_seen FROM nodes WHERE id = ?`, id)
	return scanNode(row)
}

func scanNode(row interface{ Scan(dest ...any) error }) (*models.Node, error) {
	var n models.Node
	var meta, expiresAt, sourceFile, provider sql.NullString
	var lastSeen, firstSeen string

	err := row.Scan(&n.ID, &n.Name, &n.Type, &n.Source, &sourceFile, &provider, &meta, &expiresAt, &lastSeen, &firstSeen)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	n.SourceFile = sourceFile.String
	n.Provider = provider.String

	if meta.Valid {
		_ = json.Unmarshal([]byte(meta.String), &n.Metadata)
	}
	if n.Metadata == nil {
		n.Metadata = make(map[string]string)
	}

	if expiresAt.Valid {
		t, err := time.Parse(time.RFC3339, expiresAt.String)
		if err == nil {
			n.ExpiresAt = &t
		}
	}

	n.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
	n.FirstSeen, _ = time.Parse(time.RFC3339, firstSeen)

	return &n, nil
}

// ListNodes returns nodes matching the given filter.
func (s *SQLiteStore) ListNodes(ctx context.Context, filter NodeFilter) ([]models.Node, error) {
	query := `SELECT id, name, type, source, source_file, provider, metadata, expires_at, last_seen, first_seen FROM nodes WHERE 1=1`
	var args []any

	if filter.Type != "" {
		query += ` AND type = ?`
		args = append(args, filter.Type)
	}
	if filter.Source != "" {
		query += ` AND source = ?`
		args = append(args, filter.Source)
	}
	if filter.Provider != "" {
		query += ` AND provider = ?`
		args = append(args, filter.Provider)
	}
	if filter.StaleDays > 0 {
		threshold := time.Now().Add(-time.Duration(filter.StaleDays) * 24 * time.Hour).Format(time.RFC3339)
		query += ` AND last_seen < ?`
		args = append(args, threshold)
	}

	query += ` ORDER BY type, name`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort cleanup

	var nodes []models.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, *n)
	}
	return nodes, rows.Err()
}

// ListEdges returns edges matching the given filter.
func (s *SQLiteStore) ListEdges(ctx context.Context, filter EdgeFilter) ([]models.Edge, error) {
	query := `SELECT id, from_id, to_id, type, metadata FROM edges WHERE 1=1`
	var args []any

	if filter.Type != "" {
		query += ` AND type = ?`
		args = append(args, filter.Type)
	}
	if filter.FromID != "" {
		query += ` AND from_id = ?`
		args = append(args, filter.FromID)
	}
	if filter.ToID != "" {
		query += ` AND to_id = ?`
		args = append(args, filter.ToID)
	}

	query += ` ORDER BY type, from_id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort cleanup

	var edges []models.Edge
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, err
		}
		edges = append(edges, *e)
	}
	return edges, rows.Err()
}

func scanEdge(row interface{ Scan(dest ...any) error }) (*models.Edge, error) {
	var e models.Edge
	var meta sql.NullString

	err := row.Scan(&e.ID, &e.FromID, &e.ToID, &e.Type, &meta)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if meta.Valid {
		_ = json.Unmarshal([]byte(meta.String), &e.Metadata)
	}
	if e.Metadata == nil {
		e.Metadata = make(map[string]string)
	}

	return &e, nil
}

// GetNeighbors returns all nodes connected to the given node (both directions).
func (s *SQLiteStore) GetNeighbors(ctx context.Context, nodeID string) ([]models.Node, error) {
	query := `
		SELECT DISTINCT n.id, n.name, n.type, n.source, n.source_file, n.provider, n.metadata, n.expires_at, n.last_seen, n.first_seen
		FROM nodes n
		WHERE n.id IN (
			SELECT to_id FROM edges WHERE from_id = ?
			UNION
			SELECT from_id FROM edges WHERE to_id = ?
		)
		ORDER BY n.type, n.name
	`
	rows, err := s.db.QueryContext(ctx, query, nodeID, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort cleanup

	var nodes []models.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, *n)
	}
	return nodes, rows.Err()
}

// GetEdgesFrom returns all edges originating from the given node.
func (s *SQLiteStore) GetEdgesFrom(ctx context.Context, nodeID string) ([]models.Edge, error) {
	return s.ListEdges(ctx, EdgeFilter{FromID: nodeID})
}

// GetEdgesTo returns all edges pointing to the given node.
func (s *SQLiteStore) GetEdgesTo(ctx context.Context, nodeID string) ([]models.Edge, error) {
	return s.ListEdges(ctx, EdgeFilter{ToID: nodeID})
}

// DeleteNode removes a node and its edges from the store.
func (s *SQLiteStore) DeleteNode(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
	return err
}

// NodeCount returns the total number of nodes.
func (s *SQLiteStore) NodeCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes`).Scan(&count)
	return count, err
}

// EdgeCount returns the total number of edges.
func (s *SQLiteStore) EdgeCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges`).Scan(&count)
	return count, err
}

// RecordScan inserts a new scan record and returns its ID.
func (s *SQLiteStore) RecordScan(ctx context.Context, scan Scan) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO scans (source, source_path, started_at, status) VALUES (?, ?, ?, ?)
	`, scan.Source, scan.SourcePath, scan.StartedAt.Format(time.RFC3339), scan.Status)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateScan updates a scan record with its final status and counts.
func (s *SQLiteStore) UpdateScan(ctx context.Context, id int64, status string, nodesFound, edgesFound int) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE scans SET status = ?, nodes_found = ?, edges_found = ?, finished_at = ? WHERE id = ?
	`, status, nodesFound, edgesFound, now, id)
	return err
}

// ListScans returns the most recent scan records, up to limit.
func (s *SQLiteStore) ListScans(ctx context.Context, limit int) ([]Scan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source, source_path, started_at, finished_at, nodes_found, edges_found, status
		FROM scans ORDER BY id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort cleanup

	var scans []Scan
	for rows.Next() {
		var sc Scan
		var finishedAt sql.NullString
		var startedAt string
		if err := rows.Scan(&sc.ID, &sc.Source, &sc.SourcePath, &startedAt, &finishedAt, &sc.NodesFound, &sc.EdgesFound, &sc.Status); err != nil {
			return nil, err
		}
		sc.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
		if finishedAt.Valid {
			t, _ := time.Parse(time.RFC3339, finishedAt.String)
			sc.FinishedAt = &t
		}
		scans = append(scans, sc)
	}
	return scans, rows.Err()
}

// NodeCountByType returns node counts grouped by type.
func (s *SQLiteStore) NodeCountByType(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort cleanup

	counts := make(map[string]int)
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		counts[t] = c
	}
	return counts, rows.Err()
}

// EdgeCountByType returns edge counts grouped by type.
func (s *SQLiteStore) EdgeCountByType(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT type, COUNT(*) FROM edges GROUP BY type ORDER BY type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort cleanup

	counts := make(map[string]int)
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		counts[t] = c
	}
	return counts, rows.Err()
}

// ExpiringNodes returns nodes with expiry within the given number of days.
func (s *SQLiteStore) ExpiringNodes(ctx context.Context, days int) ([]models.Node, error) {
	threshold := time.Now().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	now := time.Now().Format(time.RFC3339)

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, source, source_file, provider, metadata, expires_at, last_seen, first_seen
		FROM nodes
		WHERE expires_at IS NOT NULL AND expires_at <= ? AND expires_at >= ?
		ORDER BY expires_at
	`, threshold, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort cleanup

	var nodes []models.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, *n)
	}
	return nodes, rows.Err()
}

// AllEdges returns all edges (used by graph traversal).
func (s *SQLiteStore) AllEdges(ctx context.Context) ([]models.Edge, error) {
	return s.ListEdges(ctx, EdgeFilter{})
}

// BuildAdjacency builds an in-memory adjacency list from all edges for fast traversal.
func (s *SQLiteStore) BuildAdjacency(ctx context.Context) (downstream map[string][]models.Edge, upstream map[string][]models.Edge, err error) {
	edges, err := s.AllEdges(ctx)
	if err != nil {
		return nil, nil, err
	}

	downstream = make(map[string][]models.Edge)
	upstream = make(map[string][]models.Edge)

	for _, e := range edges {
		downstream[e.FromID] = append(downstream[e.FromID], e)
		upstream[e.ToID] = append(upstream[e.ToID], e)
	}

	return downstream, upstream, nil
}

// GenerateEdgeID creates a deterministic edge ID.
func GenerateEdgeID(fromID, toID string, edgeType models.EdgeType) string {
	return strings.Join([]string{fromID, string(edgeType), toID}, "->")
}

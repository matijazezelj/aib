package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/internal/scanner"
	"github.com/matijazezelj/aib/pkg/models"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodeCount, _ := s.store.NodeCount(ctx)
	edgeCount, _ := s.store.EdgeCount(ctx)
	nodesByType, _ := s.store.NodeCountByType(ctx)
	edgesByType, _ := s.store.EdgeCountByType(ctx)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	_, _ = fmt.Fprintf(w, "# HELP aib_nodes_total Total number of nodes in the graph.\n")
	_, _ = fmt.Fprintf(w, "# TYPE aib_nodes_total gauge\n")
	_, _ = fmt.Fprintf(w, "aib_nodes_total %d\n", nodeCount) //#nosec G705 -- integer from store, not user input

	_, _ = fmt.Fprintf(w, "# HELP aib_edges_total Total number of edges in the graph.\n")
	_, _ = fmt.Fprintf(w, "# TYPE aib_edges_total gauge\n")
	_, _ = fmt.Fprintf(w, "aib_edges_total %d\n", edgeCount) //#nosec G705 -- integer from store, not user input

	_, _ = fmt.Fprintf(w, "# HELP aib_nodes_by_type Number of nodes by asset type.\n")
	_, _ = fmt.Fprintf(w, "# TYPE aib_nodes_by_type gauge\n")
	for t, c := range nodesByType {
		_, _ = fmt.Fprintf(w, "aib_nodes_by_type{type=%q} %d\n", t, c)
	}

	_, _ = fmt.Fprintf(w, "# HELP aib_edges_by_type Number of edges by relationship type.\n")
	_, _ = fmt.Fprintf(w, "# TYPE aib_edges_by_type gauge\n")
	for t, c := range edgesByType {
		_, _ = fmt.Fprintf(w, "aib_edges_by_type{type=%q} %d\n", t, c)
	}

	expiringCerts, _ := s.tracker.ExpiringCerts(ctx, 30)
	_, _ = fmt.Fprintf(w, "# HELP aib_certs_expiring_total Certificates expiring within 30 days.\n")
	_, _ = fmt.Fprintf(w, "# TYPE aib_certs_expiring_total gauge\n")
	_, _ = fmt.Fprintf(w, "aib_certs_expiring_total %d\n", len(expiringCerts)) //#nosec G705 -- integer from store, not user input

	scans, _ := s.store.ListScans(ctx, 1000)
	completed, failed := 0, 0
	for _, sc := range scans {
		switch sc.Status {
		case "completed":
			completed++
		case "failed":
			failed++
		}
	}
	_, _ = fmt.Fprintf(w, "# HELP aib_scans_completed_total Total completed scans.\n")
	_, _ = fmt.Fprintf(w, "# TYPE aib_scans_completed_total gauge\n")
	_, _ = fmt.Fprintf(w, "aib_scans_completed_total %d\n", completed)

	_, _ = fmt.Fprintf(w, "# HELP aib_scans_failed_total Total failed scans.\n")
	_, _ = fmt.Fprintf(w, "# TYPE aib_scans_failed_total gauge\n")
	_, _ = fmt.Fprintf(w, "aib_scans_failed_total %d\n", failed)

	_, _ = fmt.Fprintf(w, "# HELP aib_build_info AIB build information.\n")
	_, _ = fmt.Fprintf(w, "# TYPE aib_build_info gauge\n")
	_, _ = fmt.Fprintf(w, "aib_build_info{version=%q} 1\n", s.version)
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, err := s.store.ListNodes(ctx, graph.NodeFilter{})
	if err != nil {
		s.logger.Error("listing nodes", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	edges, err := s.store.ListEdges(ctx, graph.EdgeFilter{})
	if err != nil {
		s.logger.Error("listing edges", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if nodes == nil {
		nodes = []models.Node{}
	}
	if edges == nil {
		edges = []models.Edge{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"edges": edges,
	})
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filter := graph.NodeFilter{
		Type:     r.URL.Query().Get("type"),
		Source:   r.URL.Query().Get("source"),
		Provider: r.URL.Query().Get("provider"),
	}

	nodes, err := s.store.ListNodes(ctx, filter)
	if err != nil {
		s.logger.Error("listing nodes", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) handleNodeByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "node id required")
		return
	}

	node, err := s.store.GetNode(ctx, id)
	if err != nil {
		s.logger.Error("getting node", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if node == nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (s *Server) handleEdges(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filter := graph.EdgeFilter{
		Type:   r.URL.Query().Get("type"),
		FromID: r.URL.Query().Get("from"),
		ToID:   r.URL.Query().Get("to"),
	}

	edges, err := s.store.ListEdges(ctx, filter)
	if err != nil {
		s.logger.Error("listing edges", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, edges)
}

func (s *Server) handleImpact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodeID := r.PathValue("nodeId")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "node id required")
		return
	}

	result, err := s.engine.BlastRadius(ctx, nodeID)
	if err != nil {
		s.logger.Error("blast radius", "nodeId", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleShortestPath(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fromID := r.URL.Query().Get("from")
	toID := r.URL.Query().Get("to")
	if fromID == "" || toID == "" {
		writeError(w, http.StatusBadRequest, "both 'from' and 'to' query parameters are required")
		return
	}

	nodes, edges, err := s.engine.ShortestPath(ctx, fromID, toID)
	if err != nil {
		s.logger.Error("shortest path", "from", fromID, "to", toID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"edges": edges,
	})
}

func (s *Server) handleDependencyChain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodeID := r.PathValue("nodeId")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "node id required")
		return
	}

	depth := 10
	if d := r.URL.Query().Get("depth"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed >= 1 && parsed <= 50 {
			depth = parsed
		}
	}

	nodes, err := s.engine.DependencyChain(ctx, nodeID, depth)
	if err != nil {
		s.logger.Error("dependency chain", "nodeId", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"depth": depth,
	})
}

func (s *Server) handleCerts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	certs, err := s.tracker.ListCerts(ctx)
	if err != nil {
		s.logger.Error("listing certs", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, certs)
}

func (s *Server) handleExpiringCerts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 && parsed <= 3650 {
			days = parsed
		}
	}

	certs, err := s.tracker.ExpiringCerts(ctx, days)
	if err != nil {
		s.logger.Error("listing expiring certs", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, certs)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodeCount, _ := s.store.NodeCount(ctx)
	edgeCount, _ := s.store.EdgeCount(ctx)
	nodesByType, _ := s.store.NodeCountByType(ctx)
	edgesByType, _ := s.store.EdgeCountByType(ctx)

	expiringCerts, _ := s.tracker.ExpiringCerts(ctx, 30)

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes_total":    nodeCount,
		"edges_total":    edgeCount,
		"nodes_by_type":  nodesByType,
		"edges_by_type":  edgesByType,
		"expiring_certs": len(expiringCerts),
	})
}

func (s *Server) handleScans(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scans, err := s.store.ListScans(ctx, 50)
	if err != nil {
		s.logger.Error("listing scans", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, scans)
}

// scanTriggerRequest is the JSON body for POST /api/v1/scan.
type scanTriggerRequest struct {
	Source     string   `json:"source"`
	Paths      []string `json:"paths,omitempty"`
	Remote     bool     `json:"remote,omitempty"`
	Workspace  string   `json:"workspace,omitempty"`
	Helm       bool     `json:"helm,omitempty"`
	ValuesFile string   `json:"values_file,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
	Playbooks  string   `json:"playbooks,omitempty"`
}

var nsRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// validatePath checks a single file path for traversal and requires absolute paths.
func validatePath(p string) error {
	cleaned := filepath.Clean(p)
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("path %q contains directory traversal", p)
	}
	if !filepath.IsAbs(cleaned) {
		return fmt.Errorf("path %q must be absolute", p)
	}
	return nil
}

// validateScanRequest checks paths and namespaces in the request.
func validateScanRequest(req scanTriggerRequest) error {
	for _, p := range req.Paths {
		if err := validatePath(p); err != nil {
			return err
		}
	}
	if req.ValuesFile != "" {
		if err := validatePath(req.ValuesFile); err != nil {
			return fmt.Errorf("values_file: %w", err)
		}
	}
	if req.Playbooks != "" {
		if err := validatePath(req.Playbooks); err != nil {
			return fmt.Errorf("playbooks: %w", err)
		}
	}
	for _, ns := range req.Namespaces {
		if !nsRegexp.MatchString(ns) {
			return fmt.Errorf("invalid namespace %q (must match [a-z0-9-]+)", ns)
		}
	}
	return nil
}

// isPathAllowed checks whether the given path falls within one of the
// configured allowed directories. If no allowlist is set, all paths are
// permitted.
func (s *Server) isPathAllowed(p string) bool {
	if len(s.allowedPaths) == 0 {
		return true
	}
	cleaned := filepath.Clean(p)
	for _, allowed := range s.allowedPaths {
		rel, err := filepath.Rel(allowed, cleaned)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

func (s *Server) handleTriggerScan(w http.ResponseWriter, r *http.Request) {
	var req scanTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	validSources := map[string]bool{
		"terraform": true, "terraform-plan": true, "kubernetes": true,
		"kubernetes-live": true, "ansible": true, "compose": true,
		"cloudformation": true, "pulumi": true, "all": true,
	}
	if !validSources[req.Source] {
		writeError(w, http.StatusBadRequest,
			"source must be one of: terraform, terraform-plan, kubernetes, kubernetes-live, ansible, compose, cloudformation, pulumi, all")
		return
	}

	if req.Source == "all" {
		if s.scanner == nil {
			writeError(w, http.StatusServiceUnavailable, "scanner not configured")
			return
		}
		scanReq := scanner.ScanRequest{Source: "all"}
		scanID, err := s.scanner.RunAsync(r.Context(), scanReq)
		if err != nil {
			s.logger.Error("triggering scan", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to start scan")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":  "scan triggered",
			"scan_id": scanID,
		})
		return
	}

	if req.Source != "kubernetes-live" && len(req.Paths) == 0 {
		writeError(w, http.StatusBadRequest, "paths required for file-based scans")
		return
	}

	if err := validateScanRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	for _, p := range req.Paths {
		if !s.isPathAllowed(p) {
			writeError(w, http.StatusForbidden, fmt.Sprintf("path %q is not in the allowed scan paths", p))
			return
		}
	}

	if s.scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "scanner not configured")
		return
	}

	scanReq := scanner.ScanRequest{
		Source:     req.Source,
		Paths:      req.Paths,
		Remote:     req.Remote,
		Workspace:  req.Workspace,
		Helm:       req.Helm,
		ValuesFile: req.ValuesFile,
		Namespaces: req.Namespaces,
		Playbooks:  req.Playbooks,
	}

	scanID, err := s.scanner.RunAsync(r.Context(), scanReq)
	if err != nil {
		s.logger.Error("triggering scan", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start scan")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "scan triggered",
		"scan_id": scanID,
	})
}

func (s *Server) handleScanDiff(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid scan ID")
		return
	}

	diff, err := s.store.GetDiff(r.Context(), id)
	if err != nil {
		s.logger.Error("getting scan diff", "scanID", id, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if diff == nil {
		writeError(w, http.StatusNotFound, "no diff found for this scan")
		return
	}

	writeJSON(w, http.StatusOK, diff)
}

func (s *Server) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	running := s.scanner != nil && s.scanner.IsRunning()
	writeJSON(w, http.StatusOK, map[string]any{"running": running})
}

func (s *Server) handleCycles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cycles, err := s.engine.FindCycles(ctx)
	if err != nil {
		s.logger.Error("finding cycles", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cycles": cycles,
		"count":  len(cycles),
	})
}

func (s *Server) handleSPOF(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	minAffected := 1
	if m := r.URL.Query().Get("min_affected"); m != "" {
		if parsed, err := strconv.Atoi(m); err == nil && parsed >= 1 {
			minAffected = parsed
		}
	}
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed >= 1 {
			limit = parsed
		}
	}

	spofs, err := s.engine.FindSPOF(ctx, minAffected)
	if err != nil {
		s.logger.Error("finding spof", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if limit > 0 && len(spofs) > limit {
		spofs = spofs[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"spof":  spofs,
		"count": len(spofs),
	})
}

func (s *Server) handleOrphans(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orphans, err := s.engine.FindOrphans(ctx)
	if err != nil {
		s.logger.Error("finding orphans", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"orphans": orphans,
		"count":   len(orphans),
	})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	report, err := graph.RunAudit(r.Context(), s.store)
	if err != nil {
		s.logger.Error("running audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if nodeID := r.URL.Query().Get("node_id"); nodeID != "" {
		report = report.FilterByNode(nodeID)
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleResolveNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hostname := r.URL.Query().Get("hostname")
	if hostname == "" {
		writeError(w, http.StatusBadRequest, "hostname query parameter required")
		return
	}

	nodes, err := s.store.ListNodes(ctx, graph.NodeFilter{})
	if err != nil {
		s.logger.Error("listing nodes for resolve", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Match any node whose ID ends with :hostname (covers k8s:node:, ansible:vm:, tf:vm:, etc.)
	// or whose name matches.
	suffix := ":" + hostname
	for _, n := range nodes {
		if strings.HasSuffix(n.ID, suffix) || n.Name == hostname {
			writeJSON(w, http.StatusOK, n)
			return
		}
	}

	writeError(w, http.StatusNotFound, "node not found")
}

// planImpactNode represents a planned resource change with its blast radius.
type planImpactNode struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	Action        string         `json:"action"`
	AffectedCount int            `json:"affected_count"`
	AffectedByType map[string]int `json:"affected_by_type,omitempty"`
}

func (s *Server) handlePlanImpact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Find all nodes from terraform-plan source.
	nodes, err := s.store.ListNodes(ctx, graph.NodeFilter{Source: "terraform-plan"})
	if err != nil {
		s.logger.Error("listing plan nodes", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var results []planImpactNode
	for _, n := range nodes {
		action := n.Metadata["plan_action"]
		pin := planImpactNode{
			ID:     n.ID,
			Name:   n.Name,
			Type:   string(n.Type),
			Action: action,
		}

		// Compute blast radius for update/delete/replace actions.
		if action == "update" || action == "delete" || action == "replace" {
			impact, err := s.engine.BlastRadius(ctx, n.ID)
			if err == nil {
				pin.AffectedCount = impact.AffectedNodes
				pin.AffectedByType = impact.AffectedByType
			}
		}

		results = append(results, pin)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"plan_nodes": results,
		"count":      len(results),
	})
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	out, err := graph.ExportJSON(r.Context(), s.store)
	if err != nil {
		s.logger.Error("export json", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="aib-graph.json"`)
	_, _ = w.Write([]byte(out)) //#nosec G705 -- data from internal store, served as file download
}

func (s *Server) handleExportDOT(w http.ResponseWriter, r *http.Request) {
	out, err := graph.ExportDOT(r.Context(), s.store)
	if err != nil {
		s.logger.Error("export dot", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "text/vnd.graphviz")
	w.Header().Set("Content-Disposition", `attachment; filename="aib-graph.dot"`)
	_, _ = w.Write([]byte(out)) //#nosec G705 -- data from internal store, served as file download
}

func (s *Server) handleExportMermaid(w http.ResponseWriter, r *http.Request) {
	out, err := graph.ExportMermaid(r.Context(), s.store)
	if err != nil {
		s.logger.Error("export mermaid", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", `attachment; filename="aib-graph.mmd"`)
	_, _ = w.Write([]byte(out)) //#nosec G705 -- data from internal store, served as file download
}

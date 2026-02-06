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
		nodes = nil
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

func (s *Server) handleTriggerScan(w http.ResponseWriter, r *http.Request) {
	if s.scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "scanner not configured")
		return
	}

	var req scanTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	validSources := map[string]bool{
		"terraform": true, "kubernetes": true,
		"kubernetes-live": true, "ansible": true, "all": true,
	}
	if !validSources[req.Source] {
		writeError(w, http.StatusBadRequest,
			"source must be one of: terraform, kubernetes, kubernetes-live, ansible, all")
		return
	}

	if req.Source == "all" {
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

func (s *Server) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	running := s.scanner != nil && s.scanner.IsRunning()
	writeJSON(w, http.StatusOK, map[string]any{"running": running})
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
	_, _ = w.Write([]byte(out))
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
	_, _ = w.Write([]byte(out))
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
	_, _ = w.Write([]byte(out))
}

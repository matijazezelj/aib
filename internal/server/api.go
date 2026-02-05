package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/matijazezelj/aib/internal/graph"
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

func (s *Server) handleTriggerScan(w http.ResponseWriter, r *http.Request) {
	// Placeholder for scan triggering via API
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "scan triggered"})
}

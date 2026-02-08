package server

import "net/http"

// RegisterRoutes registers all API routes on the given mux.
func RegisterRoutes(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/v1/graph", s.handleGraph)
	mux.HandleFunc("GET /api/v1/graph/nodes", s.handleNodes)
	mux.HandleFunc("GET /api/v1/graph/nodes/{id...}", s.handleNodeByID)
	mux.HandleFunc("GET /api/v1/graph/edges", s.handleEdges)
	mux.HandleFunc("GET /api/v1/impact/{nodeId...}", s.handleImpact)
	mux.HandleFunc("GET /api/v1/graph/shortest-path", s.handleShortestPath)
	mux.HandleFunc("GET /api/v1/graph/dependency-chain/{nodeId...}", s.handleDependencyChain)
	mux.HandleFunc("GET /api/v1/certs", s.handleCerts)
	mux.HandleFunc("GET /api/v1/certs/expiring", s.handleExpiringCerts)
	mux.HandleFunc("GET /api/v1/stats", s.handleStats)
	mux.HandleFunc("GET /api/v1/scans", s.handleScans)
	mux.HandleFunc("GET /api/v1/scan/status", s.handleScanStatus)

	mux.HandleFunc("GET /api/v1/graph/analysis/cycles", s.handleCycles)
	mux.HandleFunc("GET /api/v1/graph/analysis/spof", s.handleSPOF)
	mux.HandleFunc("GET /api/v1/graph/analysis/orphans", s.handleOrphans)

	mux.HandleFunc("GET /api/v1/export/json", s.handleExportJSON)
	mux.HandleFunc("GET /api/v1/export/dot", s.handleExportDOT)
	mux.HandleFunc("GET /api/v1/export/mermaid", s.handleExportMermaid)

	mux.HandleFunc("GET /api/v1/plan/impact", s.handlePlanImpact)

	if !s.readOnly {
		mux.HandleFunc("POST /api/v1/scan", s.handleTriggerScan)
	}
}

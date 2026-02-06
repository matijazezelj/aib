package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/matijazezelj/aib/internal/config"
	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/internal/parser"
	"github.com/matijazezelj/aib/internal/parser/ansible"
	"github.com/matijazezelj/aib/internal/parser/kubernetes"
	"github.com/matijazezelj/aib/internal/parser/terraform"
)

// ScanRequest describes a scan to execute.
type ScanRequest struct {
	Source string // "terraform", "kubernetes", "kubernetes-live", "ansible"
	Paths  []string

	// Terraform-specific
	Remote    bool
	Workspace string

	// Kubernetes-specific
	Helm       bool
	ValuesFile string
	Kubeconfig string   // for live K8s
	Context    string   // for live K8s
	Namespaces []string // for live K8s (empty = all non-system)

	// Ansible-specific
	Playbooks string
}

// ScanResult is returned after a scan completes.
type ScanResult struct {
	ScanID     int64
	NodesFound int
	EdgesFound int
	Warnings   []string
	Error      error
}

// Scanner orchestrates infrastructure scans.
type Scanner struct {
	store   *graph.SQLiteStore
	logger  *slog.Logger
	cfg     *config.Config
	mu      sync.Mutex
	running map[int64]context.CancelFunc
}

// New creates a Scanner.
func New(store *graph.SQLiteStore, cfg *config.Config, logger *slog.Logger) *Scanner {
	return &Scanner{
		store:   store,
		logger:  logger,
		cfg:     cfg,
		running: make(map[int64]context.CancelFunc),
	}
}

// RunSync executes a scan synchronously and returns the result.
func (s *Scanner) RunSync(ctx context.Context, req ScanRequest) ScanResult {
	sourcePath := strings.Join(req.Paths, ", ")
	if req.Source == "kubernetes-live" {
		sourcePath = "live-cluster"
	}

	scanID, _ := s.store.RecordScan(ctx, graph.Scan{
		Source:     req.Source,
		SourcePath: sourcePath,
		StartedAt:  time.Now(),
		Status:     "running",
	})

	result, err := s.executeScan(ctx, req)
	if err != nil {
		_ = s.store.UpdateScan(ctx, scanID, "failed", 0, 0)
		return ScanResult{ScanID: scanID, Error: err}
	}

	// Store all nodes first, then all edges
	for _, n := range result.Nodes {
		if err := s.store.UpsertNode(ctx, n); err != nil {
			s.logger.Warn("failed to store node", "id", n.ID, "error", err)
		}
	}
	for _, e := range result.Edges {
		if err := s.store.UpsertEdge(ctx, e); err != nil {
			s.logger.Warn("failed to store edge", "id", e.ID, "error", err)
		}
	}

	_ = s.store.UpdateScan(ctx, scanID, "completed", len(result.Nodes), len(result.Edges))

	return ScanResult{
		ScanID:     scanID,
		NodesFound: len(result.Nodes),
		EdgesFound: len(result.Edges),
		Warnings:   result.Warnings,
	}
}

// RunAsync launches a scan in a goroutine and returns the scan ID immediately.
func (s *Scanner) RunAsync(ctx context.Context, req ScanRequest) (int64, error) {
	sourcePath := strings.Join(req.Paths, ", ")
	if req.Source == "kubernetes-live" {
		sourcePath = "live-cluster"
	}
	if req.Source == "all" {
		sourcePath = "all-configured"
	}

	scanID, err := s.store.RecordScan(ctx, graph.Scan{
		Source:     req.Source,
		SourcePath: sourcePath,
		StartedAt:  time.Now(),
		Status:     "running",
	})
	if err != nil {
		return 0, fmt.Errorf("recording scan: %w", err)
	}

	asyncCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.running[scanID] = cancel
	s.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			s.mu.Lock()
			delete(s.running, scanID)
			s.mu.Unlock()
		}()

		// "all" runs all configured sources
		if req.Source == "all" {
			results := s.RunAllConfigured(asyncCtx)
			totalNodes, totalEdges := 0, 0
			for _, r := range results {
				totalNodes += r.NodesFound
				totalEdges += r.EdgesFound
			}
			_ = s.store.UpdateScan(asyncCtx, scanID, "completed", totalNodes, totalEdges)
			s.logger.Info("async scan (all) completed", "scanID", scanID, "nodes", totalNodes, "edges", totalEdges)
			return
		}

		result, err := s.executeScan(asyncCtx, req)
		if err != nil {
			s.logger.Error("async scan failed", "scanID", scanID, "error", err)
			_ = s.store.UpdateScan(asyncCtx, scanID, "failed", 0, 0)
			return
		}

		for _, n := range result.Nodes {
			if err := s.store.UpsertNode(asyncCtx, n); err != nil {
				s.logger.Warn("failed to store node", "id", n.ID, "error", err)
			}
		}
		for _, e := range result.Edges {
			if err := s.store.UpsertEdge(asyncCtx, e); err != nil {
				s.logger.Warn("failed to store edge", "id", e.ID, "error", err)
			}
		}

		_ = s.store.UpdateScan(asyncCtx, scanID, "completed", len(result.Nodes), len(result.Edges))
		s.logger.Info("async scan completed", "scanID", scanID, "nodes", len(result.Nodes), "edges", len(result.Edges))
	}()

	return scanID, nil
}

// RunAllConfigured runs all scans defined in the config and returns results.
func (s *Scanner) RunAllConfigured(ctx context.Context) []ScanResult {
	var results []ScanResult

	for _, src := range s.cfg.Sources.Terraform {
		paths := []string{}
		if src.Path != "" {
			paths = append(paths, src.Path)
		}
		if src.StateFile != "" {
			paths = append(paths, src.StateFile)
		}
		if len(paths) == 0 {
			continue
		}
		r := s.RunSync(ctx, ScanRequest{
			Source: "terraform",
			Paths:  paths,
		})
		results = append(results, r)
	}

	for _, src := range s.cfg.Sources.Kubernetes {
		if src.Live || (src.Kubeconfig != "" && src.Path == "") {
			r := s.RunSync(ctx, ScanRequest{
				Source:     "kubernetes-live",
				Kubeconfig: src.Kubeconfig,
				Context:    src.Context,
				Namespaces: src.Namespaces,
			})
			results = append(results, r)
		} else if src.Path != "" {
			r := s.RunSync(ctx, ScanRequest{
				Source:     "kubernetes",
				Paths:      []string{src.Path},
				Helm:       src.HelmChart != "",
				ValuesFile: src.ValuesFile,
			})
			results = append(results, r)
		}
	}

	for _, src := range s.cfg.Sources.Ansible {
		if src.Inventory == "" {
			continue
		}
		r := s.RunSync(ctx, ScanRequest{
			Source:    "ansible",
			Paths:     []string{src.Inventory},
			Playbooks: src.Playbooks,
		})
		results = append(results, r)
	}

	return results
}

// IsRunning returns true if any scan is currently in progress.
func (s *Scanner) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.running) > 0
}

// executeScan dispatches to the appropriate parser.
func (s *Scanner) executeScan(ctx context.Context, req ScanRequest) (*parser.ParseResult, error) {
	switch req.Source {
	case "terraform":
		return s.scanTerraform(ctx, req)
	case "kubernetes":
		return s.scanKubernetes(ctx, req)
	case "kubernetes-live":
		return s.scanKubernetesLive(ctx, req)
	case "ansible":
		return s.scanAnsible(ctx, req)
	case "all":
		// "all" is handled specially by RunAsync — it runs RunAllConfigured.
		// If it reaches here via RunSync, just run all configured sources.
		return nil, fmt.Errorf("use RunAllConfigured for source 'all'")
	default:
		return nil, fmt.Errorf("unknown scan source: %s", req.Source)
	}
}

func (s *Scanner) scanTerraform(ctx context.Context, req ScanRequest) (*parser.ParseResult, error) {
	if req.Remote {
		return terraform.PullRemoteMulti(ctx, req.Paths, req.Workspace)
	}

	p := terraform.NewStateParser()
	for _, path := range req.Paths {
		if !p.Supported(path) {
			return nil, fmt.Errorf("path %q is not a supported Terraform source", path)
		}
	}
	return p.ParseMulti(ctx, req.Paths)
}

func (s *Scanner) scanKubernetes(ctx context.Context, req ScanRequest) (*parser.ParseResult, error) {
	if req.Helm {
		return kubernetes.RenderHelm(ctx, req.Paths[0], req.ValuesFile)
	}

	p := kubernetes.NewK8sParser(req.ValuesFile)
	merged := &parser.ParseResult{}

	for _, path := range req.Paths {
		if !p.Supported(path) {
			return nil, fmt.Errorf("path %q is not a supported Kubernetes source", path)
		}
		result, err := p.Parse(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		merged.Nodes = append(merged.Nodes, result.Nodes...)
		merged.Edges = append(merged.Edges, result.Edges...)
		merged.Warnings = append(merged.Warnings, result.Warnings...)
	}

	return merged, nil
}

func (s *Scanner) scanKubernetesLive(ctx context.Context, req ScanRequest) (*parser.ParseResult, error) {
	return kubernetes.FetchLive(ctx, req.Kubeconfig, req.Context, req.Namespaces)
}

func (s *Scanner) scanAnsible(ctx context.Context, req ScanRequest) (*parser.ParseResult, error) {
	p := ansible.NewAnsibleParser(req.Playbooks)
	merged := &parser.ParseResult{}

	for _, path := range req.Paths {
		if !p.Supported(path) {
			return nil, fmt.Errorf("path %q is not a supported Ansible inventory", path)
		}
		result, err := p.Parse(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		merged.Nodes = append(merged.Nodes, result.Nodes...)
		merged.Edges = append(merged.Edges, result.Edges...)
		merged.Warnings = append(merged.Warnings, result.Warnings...)
	}

	return merged, nil
}

package kubernetes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/matijazezelj/aib/internal/parser"
)

// K8sParser parses Kubernetes YAML manifests and Helm charts.
type K8sParser struct {
	ValuesFile string // optional Helm values file
}

func NewK8sParser(valuesFile string) *K8sParser {
	return &K8sParser{ValuesFile: valuesFile}
}

func (p *K8sParser) Name() string {
	return "kubernetes"
}

func (p *K8sParser) Supported(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(path))
		return ext == ".yaml" || ext == ".yml"
	}

	// Check for Helm chart (Chart.yaml) or K8s manifests
	if _, err := os.Stat(filepath.Join(path, "Chart.yaml")); err == nil {
		return true
	}

	// Check for YAML files in directory
	matches, _ := filepath.Glob(filepath.Join(path, "*.yaml"))
	ymlMatches, _ := filepath.Glob(filepath.Join(path, "*.yml"))
	return len(matches)+len(ymlMatches) > 0
}

func (p *K8sParser) Parse(ctx context.Context, path string) (*parser.ParseResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	// Helm chart directory
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(path, "Chart.yaml")); err == nil {
			return RenderHelm(ctx, path, p.ValuesFile)
		}
	}

	// Plain manifest file(s)
	var files []string
	if info.IsDir() {
		if err := walkYAMLFiles(path, &files); err != nil {
			return nil, err
		}
	} else {
		files = []string{path}
	}

	result := &parser.ParseResult{}
	now := time.Now()

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reading %s: %v", f, err))
			continue
		}
		r, err := parseManifests(data, f, now)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("parsing %s: %v", f, err))
			continue
		}
		result.Nodes = append(result.Nodes, r.Nodes...)
		result.Edges = append(result.Edges, r.Edges...)
		result.Warnings = append(result.Warnings, r.Warnings...)
	}

	return result, nil
}

func walkYAMLFiles(dir string, files *[]string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			*files = append(*files, path)
		}
		return nil
	})
}

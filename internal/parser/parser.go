package parser

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/matijazezelj/aib/pkg/models"
)

// SafeResolvePath resolves a user-provided path to an absolute path and ensures
// it doesn't escape the expected root directory via symlinks or ".." components.
func SafeResolvePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}

	// EvalSymlinks resolves symlinks and cleans ".." components against the real filesystem.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("evaluating symlinks: %w", err)
	}

	return resolved, nil
}

// Parser discovers assets from an IaC source and returns nodes and edges.
type Parser interface {
	// Name returns the parser identifier (e.g., "terraform", "kubernetes").
	Name() string

	// Parse reads the source at the given path and returns discovered nodes and edges.
	Parse(ctx context.Context, path string) (*ParseResult, error)

	// Supported returns true if this parser can handle the given path.
	Supported(path string) bool
}

// ParseResult contains the output of a parse operation.
type ParseResult struct {
	Nodes    []models.Node
	Edges    []models.Edge
	Warnings []string
}

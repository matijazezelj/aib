package parser

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/matijazezelj/aib/pkg/models"
)

// SafeResolvePath resolves a user-provided path to an absolute path,
// evaluates symlinks, and cleans ".." components. Use SafeResolvePathUnder
// when you need to ensure the path stays within a root directory.
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

// SafeResolvePathUnder resolves a user-provided path and ensures it stays
// within the given root directory. Returns an error if the resolved path
// escapes root via symlinks or ".." components.
func SafeResolvePathUnder(root, path string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("evaluating root symlinks: %w", err)
	}

	resolved, err := SafeResolvePath(path)
	if err != nil {
		return "", err
	}

	// Ensure the resolved path is within the root.
	// Add trailing separator so "/opt/infraX" doesn't match root "/opt/infra".
	rootPrefix := resolvedRoot + string(filepath.Separator)
	if resolved != resolvedRoot && !strings.HasPrefix(resolved, rootPrefix) {
		return "", fmt.Errorf("path %q resolves to %q which is outside root %q", path, resolved, resolvedRoot)
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

package parser

import (
	"context"

	"github.com/matijazezelj/aib/pkg/models"
)

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

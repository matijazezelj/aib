package terraform

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/matijazezelj/aib/internal/parser"
	"github.com/matijazezelj/aib/pkg/models"
)

// tfPlan represents the top-level structure of `terraform show -json <planfile>`.
type tfPlan struct {
	FormatVersion    string             `json:"format_version"`
	TerraformVersion string             `json:"terraform_version"`
	ResourceChanges  []tfResourceChange `json:"resource_changes"`
}

// tfResourceChange represents a single resource change in a plan.
type tfResourceChange struct {
	Address      string   `json:"address"`
	Mode         string   `json:"mode"`
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	ProviderName string   `json:"provider_name"`
	Change       tfChange `json:"change"`
}

// tfChange describes the before/after state of a resource.
type tfChange struct {
	Actions      []string       `json:"actions"`
	Before       map[string]any `json:"before"`
	After        map[string]any `json:"after"`
	AfterUnknown map[string]any `json:"after_unknown"`
}

// PlanParser parses Terraform plan JSON output (from `terraform show -json`).
type PlanParser struct{}

// NewPlanParser creates a new Terraform plan parser.
func NewPlanParser() *PlanParser {
	return &PlanParser{}
}

// Name returns "terraform-plan".
func (p *PlanParser) Name() string {
	return "terraform-plan"
}

// Supported returns true if the path is a JSON file containing a format_version field.
func (p *PlanParser) Supported(path string) bool {
	if !strings.HasSuffix(path, ".json") {
		return false
	}
	data, err := os.ReadFile(path) // #nosec G304 -- paths validated by caller
	if err != nil {
		return false
	}
	var probe struct {
		FormatVersion string `json:"format_version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.FormatVersion != ""
}

// Parse parses a single plan JSON file.
func (p *PlanParser) Parse(ctx context.Context, path string) (*parser.ParseResult, error) {
	return p.ParseMulti(ctx, []string{path})
}

// ParseMulti parses multiple plan JSON files with cross-file edge resolution.
func (p *PlanParser) ParseMulti(ctx context.Context, paths []string) (*parser.ParseResult, error) {
	result := &parser.ParseResult{}

	// Phase 1: build global ref map across all plan files.
	globalRefMap := make(map[string]string)
	planData := make(map[string][]byte)
	for _, path := range paths {
		resolved, err := parser.SafeResolvePath(path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("resolving %s: %v", path, err))
			continue
		}
		data, err := os.ReadFile(resolved) // #nosec G304 -- paths validated by SafeResolvePath
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reading %s: %v", resolved, err))
			continue
		}
		planData[resolved] = data
		refs, err := buildPlanRefMap(data)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("building ref map for %s: %v", resolved, err))
			continue
		}
		for k, v := range refs {
			globalRefMap[k] = v
		}
	}

	// Phase 2: parse each plan file using the global ref map.
	for path, data := range planData {
		r, err := parsePlanBytesWithRefs(data, path, globalRefMap)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("parsing %s: %v", path, err))
			continue
		}
		result.Nodes = append(result.Nodes, r.Nodes...)
		result.Edges = append(result.Edges, r.Edges...)
		result.Warnings = append(result.Warnings, r.Warnings...)
	}

	return result, nil
}

// buildPlanRefMap builds a mapping from TF address to node ID for plan resources.
func buildPlanRefMap(data []byte) (map[string]string, error) {
	var plan tfPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	refToNodeID := make(map[string]string)
	for _, rc := range plan.ResourceChanges {
		if rc.Mode == "data" {
			continue
		}
		action := determinePlanAction(rc.Change.Actions)
		if action == "no-op" {
			continue
		}
		assetType := mapResourceType(rc.Type)
		if assetType == "" {
			continue
		}
		// Determine name from after state (for create) or before state (for delete).
		attrs := rc.Change.After
		if attrs == nil {
			attrs = rc.Change.Before
		}
		nodeID := fmt.Sprintf("tf:%s:%s", assetType, rc.Name)
		if attrs != nil {
			if n, ok := attrs["name"].(string); ok && n != "" {
				nodeID = fmt.Sprintf("tf:%s:%s", assetType, n)
			}
		}
		refToNodeID[rc.Address] = nodeID
	}
	return refToNodeID, nil
}

// parsePlanBytesWithRefs parses plan JSON bytes and creates nodes/edges.
func parsePlanBytesWithRefs(data []byte, sourcePath string, refToNodeID map[string]string) (*parser.ParseResult, error) {
	var plan tfPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	result := &parser.ParseResult{}
	now := time.Now()

	for _, rc := range plan.ResourceChanges {
		if rc.Mode == "data" {
			continue
		}

		action := determinePlanAction(rc.Change.Actions)
		if action == "no-op" {
			continue
		}

		assetType := mapResourceType(rc.Type)
		if assetType == "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("unmapped resource type: %s", rc.Address))
			continue
		}

		provider := extractProvider(rc.ProviderName)

		// Use after state for create/update, before for delete.
		attrs := rc.Change.After
		if attrs == nil {
			attrs = rc.Change.Before
		}
		if attrs == nil {
			attrs = make(map[string]any)
		}

		nodeID := fmt.Sprintf("tf:%s:%s", assetType, rc.Name)
		name := rc.Name
		if n, ok := attrs["name"].(string); ok && n != "" {
			name = n
			nodeID = fmt.Sprintf("tf:%s:%s", assetType, n)
		}

		meta := extractMetadata(rc.Type, attrs)
		meta["plan_action"] = action

		node := models.Node{
			ID:         nodeID,
			Name:       name,
			Type:       assetType,
			Source:     "terraform-plan",
			SourceFile: sourcePath,
			Provider:   provider,
			Metadata:   meta,
			LastSeen:   now,
			FirstSeen:  now,
		}

		result.Nodes = append(result.Nodes, node)

		// Create edges based on attribute references.
		createAttributeEdges(nodeID, rc.Type, attrs, result, refToNodeID)
	}

	return result, nil
}

// determinePlanAction classifies the action(s) for a resource change.
func determinePlanAction(actions []string) string {
	if len(actions) == 0 {
		return "no-op"
	}
	if len(actions) == 1 {
		switch actions[0] {
		case "create":
			return "create"
		case "delete":
			return "delete"
		case "update":
			return "update"
		case "read":
			return "no-op"
		case "no-op":
			return "no-op"
		}
	}
	// ["delete", "create"] = replace
	if len(actions) == 2 {
		return "replace"
	}
	return actions[0]
}

// ParsePlanBytes parses raw plan JSON bytes (exported for testing).
func ParsePlanBytes(data []byte, sourcePath string) (*parser.ParseResult, error) {
	refs, err := buildPlanRefMap(data)
	if err != nil {
		return nil, err
	}
	return parsePlanBytesWithRefs(data, sourcePath, refs)
}

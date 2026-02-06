package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/matijazezelj/aib/internal/parser"
	"github.com/matijazezelj/aib/pkg/models"
)

// pulledState holds raw bytes pulled from a remote backend, tagged with a label.
type pulledState struct {
	label string // e.g. "project-a" or "project-a/staging"
	data  []byte
}

// pullStateBytes runs `terraform state pull` and returns raw JSON bytes.
func pullStateBytes(ctx context.Context, projectDir, workspace string) ([]byte, error) {
	if _, err := exec.LookPath("terraform"); err != nil {
		return nil, fmt.Errorf("terraform CLI not found in PATH: %w", err)
	}

	if workspace != "" {
		wsCmd := exec.CommandContext(ctx, "terraform", fmt.Sprintf("-chdir=%s", projectDir), "workspace", "select", workspace) // #nosec G204 -- args are constructed internally
		var wsErr bytes.Buffer
		wsCmd.Stderr = &wsErr
		if err := wsCmd.Run(); err != nil {
			return nil, fmt.Errorf("selecting workspace %q: %s", workspace, wsErr.String())
		}
	}

	pullCmd := exec.CommandContext(ctx, "terraform", fmt.Sprintf("-chdir=%s", projectDir), "state", "pull") // #nosec G204 -- args are constructed internally
	var stdout, stderr bytes.Buffer
	pullCmd.Stdout = &stdout
	pullCmd.Stderr = &stderr

	if err := pullCmd.Run(); err != nil {
		return nil, fmt.Errorf("terraform state pull failed: %s", stderr.String())
	}

	if stdout.Len() == 0 {
		return nil, fmt.Errorf("terraform state pull returned empty output (no state found)")
	}

	// Verify it's valid JSON
	var state tfState
	if err := json.Unmarshal(stdout.Bytes(), &state); err != nil {
		return nil, fmt.Errorf("parsing state pull output: %w", err)
	}

	return stdout.Bytes(), nil
}

// PullRemoteState runs `terraform state pull` in the given directory
// and returns the parsed state as a ParseResult.
func PullRemoteState(ctx context.Context, projectDir string, workspace string) (*parser.ParseResult, error) {
	data, err := pullStateBytes(ctx, projectDir, workspace)
	if err != nil {
		return nil, err
	}
	return parseStateBytes(data, projectDir)
}

// PullRemoteMulti pulls state from multiple project directories with cross-state
// edge resolution. When workspace is "*", all workspaces are pulled from each path.
func PullRemoteMulti(ctx context.Context, projectDirs []string, workspace string) (*parser.ParseResult, error) {
	// Collect raw state bytes from all sources
	var states []pulledState
	var warnings []string

	for _, dir := range projectDirs {
		if workspace == "*" {
			workspaces, err := ListWorkspaces(ctx, dir)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("listing workspaces in %s: %v", dir, err))
				continue
			}
			for _, ws := range workspaces {
				fmt.Printf("Pulling state from %s (workspace: %s)...\n", dir, ws)
				data, err := pullStateBytes(ctx, dir, ws)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("%s workspace %q: %v", dir, ws, err))
					continue
				}
				states = append(states, pulledState{label: dir + "/" + ws, data: data})
			}
		} else {
			wsLabel := "default"
			if workspace != "" {
				wsLabel = workspace
			}
			fmt.Printf("Pulling remote state from %s (workspace: %s)...\n", dir, wsLabel)
			data, err := pullStateBytes(ctx, dir, workspace)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", dir, err))
				continue
			}
			states = append(states, pulledState{label: dir, data: data})
		}
	}

	// Phase 1: build global ref map across all pulled states
	globalRefMap := make(map[string]string)
	for _, s := range states {
		refs, err := buildRefMap(s.data)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("building ref map for %s: %v", s.label, err))
			continue
		}
		for k, v := range refs {
			globalRefMap[k] = v
		}
	}

	// Phase 2: parse each state with the global ref map
	result := &parser.ParseResult{Warnings: warnings}
	for _, s := range states {
		r, err := parseStateBytesWithRefs(s.data, s.label, globalRefMap)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("parsing %s: %v", s.label, err))
			continue
		}
		result.Nodes = append(result.Nodes, r.Nodes...)
		result.Edges = append(result.Edges, r.Edges...)
		result.Warnings = append(result.Warnings, r.Warnings...)
	}

	return result, nil
}

// ListWorkspaces returns the list of terraform workspaces in a project directory.
func ListWorkspaces(ctx context.Context, projectDir string) ([]string, error) {
	if _, err := exec.LookPath("terraform"); err != nil {
		return nil, fmt.Errorf("terraform CLI not found in PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, "terraform", fmt.Sprintf("-chdir=%s", projectDir), "workspace", "list") // #nosec G204 -- args are constructed internally
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("terraform workspace list: %s", stderr.String())
	}

	var workspaces []string
	for _, line := range bytes.Split(stdout.Bytes(), []byte("\n")) {
		ws := string(bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("*"))))
		if ws != "" {
			workspaces = append(workspaces, ws)
		}
	}
	return workspaces, nil
}

// PullAllWorkspaces pulls state from all workspaces in a project with
// cross-workspace edge resolution.
func PullAllWorkspaces(ctx context.Context, projectDir string) (*parser.ParseResult, error) {
	return PullRemoteMulti(ctx, []string{projectDir}, "*")
}

// parseStateBytes parses raw JSON state bytes (shared between local and remote).
// It builds a local ref map and resolves edges within the single file.
func parseStateBytes(data []byte, sourcePath string) (*parser.ParseResult, error) {
	refs, err := buildRefMap(data)
	if err != nil {
		return nil, err
	}
	return parseStateBytesWithRefs(data, sourcePath, refs)
}

// buildRefMap performs the first pass over a state file: builds a mapping
// from TF block names (e.g. "google_compute_network.prod_vpc") to node IDs
// (e.g. "tf:network:prod-vpc").
func buildRefMap(data []byte) (map[string]string, error) {
	var state tfState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	refToNodeID := make(map[string]string)
	for _, res := range state.Resources {
		if res.Mode == "data" {
			continue
		}
		assetType := mapResourceType(res.Type)
		if assetType == "" {
			continue
		}
		for _, inst := range res.Instances {
			nodeID := fmt.Sprintf("tf:%s:%s", assetType, res.Name)
			if n, ok := inst.Attributes["name"].(string); ok && n != "" {
				nodeID = fmt.Sprintf("tf:%s:%s", assetType, n)
			}
			ref := res.Type + "." + res.Name
			refToNodeID[ref] = nodeID
		}
	}
	return refToNodeID, nil
}

// parseStateBytesWithRefs performs the second pass: creates nodes and edges
// using the provided refToNodeID map (which may span multiple state files).
func parseStateBytesWithRefs(data []byte, sourcePath string, refToNodeID map[string]string) (*parser.ParseResult, error) {
	var state tfState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	result := &parser.ParseResult{}
	now := time.Now()

	for _, res := range state.Resources {
		if res.Mode == "data" {
			continue
		}

		assetType := mapResourceType(res.Type)
		if assetType == "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("unmapped resource type: %s.%s", res.Type, res.Name))
			continue
		}

		provider := extractProvider(res.Provider)

		for _, inst := range res.Instances {
			nodeID := fmt.Sprintf("tf:%s:%s", assetType, res.Name)
			name := res.Name
			if n, ok := inst.Attributes["name"].(string); ok && n != "" {
				name = n
				nodeID = fmt.Sprintf("tf:%s:%s", assetType, n)
			}

			node := models.Node{
				ID:         nodeID,
				Name:       name,
				Type:       assetType,
				Source:     "terraform",
				SourceFile: sourcePath,
				Provider:   provider,
				Metadata:   extractMetadata(res.Type, inst.Attributes),
				LastSeen:   now,
				FirstSeen:  now,
			}

			if assetType == models.AssetCertificate {
				if exp, ok := inst.Attributes["not_after"].(string); ok {
					if t, err := time.Parse(time.RFC3339, exp); err == nil {
						node.ExpiresAt = &t
					}
				}
			}

			result.Nodes = append(result.Nodes, node)

			for _, dep := range inst.Dependencies {
				depNodeID, ok := refToNodeID[dep]
				if !ok {
					continue
				}
				edgeID := fmt.Sprintf("%s->depends_on->%s", nodeID, depNodeID)
				result.Edges = append(result.Edges, models.Edge{
					ID:     edgeID,
					FromID: nodeID,
					ToID:   depNodeID,
					Type:   models.EdgeDependsOn,
					Metadata: map[string]string{
						"source": "tfstate_dependency",
					},
				})
			}

			createAttributeEdges(nodeID, res.Type, inst.Attributes, result, refToNodeID)
		}
	}

	return result, nil
}

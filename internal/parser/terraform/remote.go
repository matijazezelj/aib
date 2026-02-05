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

// PullRemoteState runs `terraform state pull` in the given directory
// and returns the parsed state as a ParseResult.
func PullRemoteState(ctx context.Context, projectDir string, workspace string) (*parser.ParseResult, error) {
	// Verify terraform is installed
	if _, err := exec.LookPath("terraform"); err != nil {
		return nil, fmt.Errorf("terraform CLI not found in PATH: %w", err)
	}

	// Optionally switch workspace before pulling
	if workspace != "" {
		wsCmd := exec.CommandContext(ctx, "terraform", fmt.Sprintf("-chdir=%s", projectDir), "workspace", "select", workspace)
		var wsErr bytes.Buffer
		wsCmd.Stderr = &wsErr
		if err := wsCmd.Run(); err != nil {
			return nil, fmt.Errorf("selecting workspace %q: %s", workspace, wsErr.String())
		}
	}

	// Run terraform state pull
	pullCmd := exec.CommandContext(ctx, "terraform", fmt.Sprintf("-chdir=%s", projectDir), "state", "pull")
	var stdout, stderr bytes.Buffer
	pullCmd.Stdout = &stdout
	pullCmd.Stderr = &stderr

	if err := pullCmd.Run(); err != nil {
		return nil, fmt.Errorf("terraform state pull failed: %s", stderr.String())
	}

	if stdout.Len() == 0 {
		return nil, fmt.Errorf("terraform state pull returned empty output (no state found)")
	}

	// Verify it's valid JSON state
	var state tfState
	if err := json.Unmarshal(stdout.Bytes(), &state); err != nil {
		return nil, fmt.Errorf("parsing state pull output: %w", err)
	}

	// Parse using the same logic as local state files
	return parseStateBytes(stdout.Bytes(), projectDir)
}

// ListWorkspaces returns the list of terraform workspaces in a project directory.
func ListWorkspaces(ctx context.Context, projectDir string) ([]string, error) {
	if _, err := exec.LookPath("terraform"); err != nil {
		return nil, fmt.Errorf("terraform CLI not found in PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, "terraform", fmt.Sprintf("-chdir=%s", projectDir), "workspace", "list")
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

// PullAllWorkspaces pulls state from all workspaces in a project and merges results.
func PullAllWorkspaces(ctx context.Context, projectDir string) (*parser.ParseResult, error) {
	workspaces, err := ListWorkspaces(ctx, projectDir)
	if err != nil {
		return nil, err
	}

	if len(workspaces) == 0 {
		return nil, fmt.Errorf("no workspaces found in %s", projectDir)
	}

	result := &parser.ParseResult{}
	for _, ws := range workspaces {
		r, err := PullRemoteState(ctx, projectDir, ws)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("workspace %q: %v", ws, err))
			continue
		}
		result.Nodes = append(result.Nodes, r.Nodes...)
		result.Edges = append(result.Edges, r.Edges...)
		result.Warnings = append(result.Warnings, r.Warnings...)
	}

	return result, nil
}

// parseStateBytes parses raw JSON state bytes (shared between local and remote).
func parseStateBytes(data []byte, sourcePath string) (*parser.ParseResult, error) {
	var state tfState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	result := &parser.ParseResult{}
	now := time.Now()

	// First pass: build ref→nodeID mapping.
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

	// Second pass: create nodes and edges.
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

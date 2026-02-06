package ansible

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/matijazezelj/aib/internal/parser"
	"github.com/matijazezelj/aib/pkg/models"
)

// AnsibleParser parses Ansible inventory files and playbooks.
type AnsibleParser struct {
	PlaybookDir string
}

// NewAnsibleParser creates a parser with an optional playbook directory.
func NewAnsibleParser(playbookDir string) *AnsibleParser {
	return &AnsibleParser{PlaybookDir: playbookDir}
}

// Name returns "ansible".
func (p *AnsibleParser) Name() string {
	return "ansible"
}

// Supported returns true if the path contains Ansible inventory files.
func (p *AnsibleParser) Supported(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		for _, name := range []string{"hosts", "inventory", "inventory.ini", "inventory.yml", "inventory.yaml"} {
			if _, err := os.Stat(filepath.Join(path, name)); err == nil {
				return true
			}
		}
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".ini" || ext == ".yml" || ext == ".yaml" || ext == ""
}

// Parse reads Ansible inventory and optional playbooks at the given path.
func (p *AnsibleParser) Parse(ctx context.Context, path string) (*parser.ParseResult, error) {
	path, err := parser.SafeResolvePath(path)
	if err != nil {
		return nil, err
	}

	result := &parser.ParseResult{}
	now := time.Now()

	inventoryFiles, err := resolveInventoryFiles(path)
	if err != nil {
		return nil, fmt.Errorf("resolving inventory: %w", err)
	}

	var allHosts []hostEntry
	for _, invFile := range inventoryFiles {
		hosts, warnings, err := parseInventoryFile(invFile)
		result.Warnings = append(result.Warnings, warnings...)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to parse %s: %v", invFile, err))
			continue
		}
		allHosts = append(allHosts, hosts...)
	}

	hostMap := deduplicateHosts(allHosts)

	// Deterministic output order
	var hostnames []string
	for h := range hostMap {
		hostnames = append(hostnames, h)
	}
	sort.Strings(hostnames)

	for _, hostname := range hostnames {
		host := hostMap[hostname]
		node := models.Node{
			ID:         fmt.Sprintf("ansible:vm:%s", hostname),
			Name:       hostname,
			Type:       models.AssetVM,
			Source:     "ansible",
			SourceFile: host.sourceFile,
			Provider:   inferProvider(host),
			Metadata:   buildHostMetadata(host),
			LastSeen:   now,
			FirstSeen:  now,
		}
		result.Nodes = append(result.Nodes, node)
	}

	// Parse playbooks if configured
	if p.PlaybookDir != "" {
		pbDir, err := parser.SafeResolvePath(p.PlaybookDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("playbook path: %v", err))
			return result, nil
		}
		pbResult, err := parsePlaybooksDir(ctx, pbDir, hostMap, now)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("playbook parsing: %v", err))
		} else {
			result.Nodes = append(result.Nodes, pbResult.Nodes...)
			result.Edges = append(result.Edges, pbResult.Edges...)
			result.Warnings = append(result.Warnings, pbResult.Warnings...)
		}
	}

	return result, nil
}

func resolveInventoryFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	for _, name := range []string{"hosts", "inventory", "inventory.ini", "inventory.yml", "inventory.yaml"} {
		candidate := filepath.Join(path, name)
		if _, err := os.Stat(candidate); err == nil {
			files = append(files, candidate)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no inventory files found in %s", path)
	}
	return files, nil
}

func inferProvider(h hostEntry) string {
	host := h.vars["ansible_host"]
	if host == "" {
		host = h.hostname
	}
	if strings.Contains(host, "amazonaws.com") {
		return "aws"
	}
	if strings.Contains(host, "googleusercontent.com") || strings.Contains(host, "gcp") {
		return "gcp"
	}
	if strings.Contains(host, "azure") {
		return "azure"
	}
	return "local"
}

func buildHostMetadata(h hostEntry) map[string]string {
	meta := make(map[string]string)
	for k, v := range h.vars {
		meta[k] = v
	}
	if len(h.groups) > 0 {
		sort.Strings(h.groups)
		meta["groups"] = strings.Join(h.groups, ",")
	}
	return meta
}

func deduplicateHosts(hosts []hostEntry) map[string]hostEntry {
	result := make(map[string]hostEntry)
	for _, h := range hosts {
		existing, ok := result[h.hostname]
		if !ok {
			result[h.hostname] = h
			continue
		}
		groupSet := make(map[string]bool)
		for _, g := range existing.groups {
			groupSet[g] = true
		}
		for _, g := range h.groups {
			groupSet[g] = true
		}
		var merged []string
		for g := range groupSet {
			merged = append(merged, g)
		}
		existing.groups = merged
		for k, v := range h.vars {
			existing.vars[k] = v
		}
		result[h.hostname] = existing
	}
	return result
}

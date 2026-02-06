package ansible

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/matijazezelj/aib/internal/parser"
	"github.com/matijazezelj/aib/pkg/models"
	"go.yaml.in/yaml/v3"
)

type ansiblePlay struct {
	Name  string        `yaml:"name"`
	Hosts string        `yaml:"hosts"`
	Tasks []ansibleTask `yaml:"tasks"`
}

// ansibleTask checks known module keys for infrastructure-relevant tasks.
type ansibleTask struct {
	Name                     string                 `yaml:"name"`
	DockerContainer          map[string]interface{} `yaml:"docker_container"`
	CommunityDockerContainer map[string]interface{} `yaml:"community.docker.docker_container"`
	Apt                      map[string]interface{} `yaml:"apt"`
	Yum                      map[string]interface{} `yaml:"yum"`
	Dnf                      map[string]interface{} `yaml:"dnf"`
	Service                  map[string]interface{} `yaml:"service"`
}

func parsePlaybooksDir(ctx context.Context, dir string, hostMap map[string]hostEntry, now time.Time) (*parser.ParseResult, error) {
	result := &parser.ParseResult{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading playbook dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yml" && ext != ".yaml" {
			continue
		}

		pbPath := filepath.Join(dir, entry.Name())
		pbResult, err := parsePlaybookFile(ctx, pbPath, hostMap, now)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("playbook %s: %v", pbPath, err))
			continue
		}
		result.Nodes = append(result.Nodes, pbResult.Nodes...)
		result.Edges = append(result.Edges, pbResult.Edges...)
		result.Warnings = append(result.Warnings, pbResult.Warnings...)
	}

	return result, nil
}

func parsePlaybookFile(ctx context.Context, path string, hostMap map[string]hostEntry, now time.Time) (*parser.ParseResult, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- paths validated by SafeResolvePath
	if err != nil {
		return nil, err
	}

	var plays []ansiblePlay
	if err := yaml.Unmarshal(data, &plays); err != nil {
		return nil, fmt.Errorf("parsing playbook YAML: %w", err)
	}

	result := &parser.ParseResult{}

	for _, play := range plays {
		targetHosts := resolveHostPattern(play.Hosts, hostMap)

		for _, task := range play.Tasks {
			dockerMod := task.DockerContainer
			if dockerMod == nil {
				dockerMod = task.CommunityDockerContainer
			}
			if dockerMod != nil {
				containerName := stringFromMap(dockerMod, "name")
				if containerName == "" {
					continue
				}
				containerID := fmt.Sprintf("ansible:container:%s", containerName)
				node := models.Node{
					ID:         containerID,
					Name:       containerName,
					Type:       models.AssetContainer,
					Source:     "ansible",
					SourceFile: path,
					Provider:   "docker",
					Metadata:   extractDockerMetadata(dockerMod),
					LastSeen:   now,
					FirstSeen:  now,
				}
				result.Nodes = append(result.Nodes, node)

				for _, hostname := range targetHosts {
					hostNodeID := fmt.Sprintf("ansible:vm:%s", hostname)
					edgeID := fmt.Sprintf("%s->managed_by->%s", containerID, hostNodeID)
					result.Edges = append(result.Edges, models.Edge{
						ID:       edgeID,
						FromID:   containerID,
						ToID:     hostNodeID,
						Type:     models.EdgeManagedBy,
						Metadata: map[string]string{"task": task.Name, "module": "docker_container"},
					})
				}
			}

			// Service module → create service dependency edges
			if task.Service != nil {
				svcName := stringFromMap(task.Service, "name")
				if svcName != "" {
					svcID := fmt.Sprintf("ansible:service:%s", svcName)
					state := stringFromMap(task.Service, "state")
					node := models.Node{
						ID:         svcID,
						Name:       svcName,
						Type:       models.AssetService,
						Source:     "ansible",
						SourceFile: path,
						Provider:   "systemd",
						Metadata:   map[string]string{"state": state},
						LastSeen:   now,
						FirstSeen:  now,
					}
					result.Nodes = append(result.Nodes, node)

					for _, hostname := range targetHosts {
						hostNodeID := fmt.Sprintf("ansible:vm:%s", hostname)
						edgeID := fmt.Sprintf("%s->managed_by->%s", svcID, hostNodeID)
						result.Edges = append(result.Edges, models.Edge{
							ID:       edgeID,
							FromID:   svcID,
							ToID:     hostNodeID,
							Type:     models.EdgeManagedBy,
							Metadata: map[string]string{"task": task.Name, "module": "service"},
						})
					}
				}
			}
		}
	}

	return result, nil
}

// resolveHostPattern resolves an Ansible hosts pattern to actual hostnames.
// Supports: "all", specific hostname, group name, comma-separated lists.
func resolveHostPattern(pattern string, hostMap map[string]hostEntry) []string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "all" || pattern == "*" {
		var names []string
		for hostname := range hostMap {
			names = append(names, hostname)
		}
		return names
	}

	// Comma-separated list
	if strings.Contains(pattern, ",") {
		var result []string
		seen := make(map[string]bool)
		for _, part := range strings.Split(pattern, ",") {
			for _, h := range resolveHostPattern(strings.TrimSpace(part), hostMap) {
				if !seen[h] {
					result = append(result, h)
					seen[h] = true
				}
			}
		}
		return result
	}

	// Specific host
	if _, ok := hostMap[pattern]; ok {
		return []string{pattern}
	}

	// Group name
	var matched []string
	for hostname, h := range hostMap {
		for _, g := range h.groups {
			if g == pattern {
				matched = append(matched, hostname)
				break
			}
		}
	}
	return matched
}

func stringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func extractDockerMetadata(m map[string]interface{}) map[string]string {
	meta := make(map[string]string)
	for _, key := range []string{"name", "image", "state", "restart_policy", "network_mode"} {
		if v := stringFromMap(m, key); v != "" {
			meta[key] = v
		}
	}
	return meta
}

package compose

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
	"gopkg.in/yaml.v3"
)

// composeFile represents the top-level structure of a Docker Compose file.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
	Networks map[string]any            `yaml:"networks"`
	Volumes  map[string]any            `yaml:"volumes"`
}

// composeService represents a single service in a Docker Compose file.
type composeService struct {
	Image      string            `yaml:"image"`
	DependsOn  dependsOn         `yaml:"depends_on"`
	Networks   serviceNetworks   `yaml:"networks"`
	Volumes    []string          `yaml:"volumes"`
	Ports      []string          `yaml:"ports"`
	Environment any              `yaml:"environment"`
}

// dependsOn handles both []string and map[string]{condition:...} forms.
type dependsOn struct {
	Services []string
}

func (d *dependsOn) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		return node.Decode(&d.Services)
	case yaml.MappingNode:
		var m map[string]any
		if err := node.Decode(&m); err != nil {
			return err
		}
		for k := range m {
			d.Services = append(d.Services, k)
		}
		sort.Strings(d.Services)
		return nil
	default:
		return fmt.Errorf("unsupported depends_on type: %v", node.Kind)
	}
}

// serviceNetworks handles both []string and map[string]{...} forms.
type serviceNetworks struct {
	Names []string
}

func (n *serviceNetworks) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		return node.Decode(&n.Names)
	case yaml.MappingNode:
		var m map[string]any
		if err := node.Decode(&m); err != nil {
			return err
		}
		for k := range m {
			n.Names = append(n.Names, k)
		}
		sort.Strings(n.Names)
		return nil
	default:
		return fmt.Errorf("unsupported networks type: %v", node.Kind)
	}
}

var composeFileNames = []string{
	"docker-compose.yml",
	"docker-compose.yaml",
	"compose.yml",
	"compose.yaml",
}

// ComposeParser parses Docker Compose files.
type ComposeParser struct{}

// NewComposeParser creates a new Docker Compose parser.
func NewComposeParser() *ComposeParser {
	return &ComposeParser{}
}

// Name returns "compose".
func (p *ComposeParser) Name() string {
	return "compose"
}

// Supported returns true if the path is a Docker Compose file or a directory containing one.
func (p *ComposeParser) Supported(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	if !info.IsDir() {
		base := filepath.Base(path)
		for _, name := range composeFileNames {
			if strings.EqualFold(base, name) {
				return true
			}
		}
		return false
	}

	// Check for compose files in directory
	for _, name := range composeFileNames {
		if _, err := os.Stat(filepath.Join(path, name)); err == nil {
			return true
		}
	}
	return false
}

// Parse reads a Docker Compose file and returns discovered nodes and edges.
func (p *ComposeParser) Parse(ctx context.Context, path string) (*parser.ParseResult, error) {
	path, err := parser.SafeResolvePath(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	// If directory, find the compose file
	if info.IsDir() {
		found := false
		for _, name := range composeFileNames {
			candidate := filepath.Join(path, name)
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("no docker compose file found in %s", path)
		}
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path validated by SafeResolvePath
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return buildGraph(cf, path), nil
}

func buildGraph(cf composeFile, sourceFile string) *parser.ParseResult {
	now := time.Now()
	result := &parser.ParseResult{}

	// Create service nodes
	for name, svc := range cf.Services {
		nodeID := "compose:container:" + name
		meta := map[string]string{}
		if svc.Image != "" {
			meta["image"] = svc.Image
		}
		if len(svc.Ports) > 0 {
			meta["ports"] = strings.Join(svc.Ports, ",")
		}

		result.Nodes = append(result.Nodes, models.Node{
			ID:         nodeID,
			Name:       name,
			Type:       models.AssetContainer,
			Source:     "compose",
			SourceFile: sourceFile,
			Provider:   "docker",
			Metadata:   meta,
			LastSeen:   now,
			FirstSeen:  now,
		})
	}

	// Create network nodes
	for name := range cf.Networks {
		nodeID := "compose:network:" + name
		result.Nodes = append(result.Nodes, models.Node{
			ID:         nodeID,
			Name:       name,
			Type:       models.AssetNetwork,
			Source:     "compose",
			SourceFile: sourceFile,
			Provider:   "docker",
			Metadata:   map[string]string{},
			LastSeen:   now,
			FirstSeen:  now,
		})
	}

	// Create volume nodes
	for name := range cf.Volumes {
		nodeID := "compose:volume:" + name
		result.Nodes = append(result.Nodes, models.Node{
			ID:         nodeID,
			Name:       name,
			Type:       models.AssetDisk,
			Source:     "compose",
			SourceFile: sourceFile,
			Provider:   "docker",
			Metadata:   map[string]string{},
			LastSeen:   now,
			FirstSeen:  now,
		})
	}

	// Create edges
	for name, svc := range cf.Services {
		fromID := "compose:container:" + name

		// depends_on edges
		for _, dep := range svc.DependsOn.Services {
			toID := "compose:container:" + dep
			edgeID := fromID + "->depends_on->" + toID
			result.Edges = append(result.Edges, models.Edge{
				ID:       edgeID,
				FromID:   fromID,
				ToID:     toID,
				Type:     models.EdgeDependsOn,
				Metadata: map[string]string{},
			})
		}

		// network edges
		for _, net := range svc.Networks.Names {
			toID := "compose:network:" + net
			edgeID := fromID + "->connects_to->" + toID
			result.Edges = append(result.Edges, models.Edge{
				ID:       edgeID,
				FromID:   fromID,
				ToID:     toID,
				Type:     models.EdgeConnectsTo,
				Metadata: map[string]string{},
			})
		}

		// volume edges
		for _, vol := range svc.Volumes {
			// volumes can be "name:/path" or "/host:/container"
			parts := strings.SplitN(vol, ":", 2)
			volName := parts[0]
			// Only create edge if it's a named volume (not a host path)
			if strings.HasPrefix(volName, "/") || strings.HasPrefix(volName, ".") {
				continue
			}
			if _, ok := cf.Volumes[volName]; !ok {
				continue
			}
			toID := "compose:volume:" + volName
			edgeID := fromID + "->mounts_secret->" + toID
			result.Edges = append(result.Edges, models.Edge{
				ID:       edgeID,
				FromID:   fromID,
				ToID:     toID,
				Type:     models.EdgeMountsSecret,
				Metadata: map[string]string{},
			})
		}
	}

	return result
}

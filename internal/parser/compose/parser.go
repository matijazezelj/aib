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
	Include  includeList               `yaml:"include"`
	Services map[string]composeService `yaml:"services"`
	Networks map[string]any            `yaml:"networks"`
	Volumes  map[string]any            `yaml:"volumes"`
}

// composeService represents a single service in a Docker Compose file.
type composeService struct {
	Image       string          `yaml:"image"`
	DependsOn   dependsOn       `yaml:"depends_on"`
	Networks    serviceNetworks `yaml:"networks"`
	Volumes     volumeList      `yaml:"volumes"`
	Ports       portList        `yaml:"ports"`
	Init        any             `yaml:"init"`
	Healthcheck any             `yaml:"healthcheck"`
	Environment any             `yaml:"environment"`
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

	var warnings []string
	visited := map[string]bool{path: true}
	if err := p.mergeIncludes(&cf, path, filepath.Dir(path), visited, 0, &warnings); err != nil {
		return nil, err
	}

	result := buildGraph(cf, path)
	result.Warnings = append(result.Warnings, warnings...)
	return result, nil
}

// maxIncludeDepth bounds `include:` recursion. Compose itself has no limit, but
// a cyclic or absurdly nested tree is a configuration error here, not something
// to follow until the stack runs out.
const maxIncludeDepth = 8

// mergeIncludes folds the services, networks and volumes of every `include:`d
// file into cf. Included files must stay inside root (the directory of the file
// the scan started from), so a compose file cannot make the scanner read
// arbitrary paths. Problems become warnings, not failures: one broken include
// should not hide the rest of the estate.
func (p *ComposeParser) mergeIncludes(cf *composeFile, from, root string, visited map[string]bool, depth int, warnings *[]string) error {
	if len(cf.Include) == 0 {
		return nil
	}
	if depth >= maxIncludeDepth {
		*warnings = append(*warnings, fmt.Sprintf("%s: include depth limit (%d) reached, deeper includes skipped", from, maxIncludeDepth))
		return nil
	}
	for _, entry := range cf.Include {
		if len(entry.Paths) == 0 {
			continue
		}
		rel := entry.Paths[0]
		target := rel
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(from), rel)
		}
		resolved, err := parser.SafeResolvePath(target)
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("%s: include %q not readable: %v", from, rel, err))
			continue
		}
		if r, err := filepath.Rel(root, resolved); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			*warnings = append(*warnings, fmt.Sprintf("%s: include %q is outside the scanned directory, skipped", from, rel))
			continue
		}
		if visited[resolved] {
			continue
		}
		visited[resolved] = true
		data, err := os.ReadFile(resolved) // #nosec G304 -- confined to root above
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("%s: include %q: %v", from, rel, err))
			continue
		}
		var inc composeFile
		if err := yaml.Unmarshal(data, &inc); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("%s: include %q does not parse: %v", from, rel, err))
			continue
		}
		if err := p.mergeIncludes(&inc, resolved, root, visited, depth+1, warnings); err != nil {
			return err
		}
		if cf.Services == nil {
			cf.Services = map[string]composeService{}
		}
		for k, v := range inc.Services {
			if _, exists := cf.Services[k]; exists {
				*warnings = append(*warnings, fmt.Sprintf("service %q defined in both %s and an include; keeping the first", k, from))
				continue
			}
			cf.Services[k] = v
		}
		if cf.Networks == nil {
			cf.Networks = map[string]any{}
		}
		for k, v := range inc.Networks {
			if _, ok := cf.Networks[k]; !ok {
				cf.Networks[k] = v
			}
		}
		if cf.Volumes == nil {
			cf.Volumes = map[string]any{}
		}
		for k, v := range inc.Volumes {
			if _, ok := cf.Volumes[k]; !ok {
				cf.Volumes[k] = v
			}
		}
	}
	return nil
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
		if svc.Init != nil {
			meta["init"] = fmt.Sprint(svc.Init)
		}
		if svc.Healthcheck != nil {
			meta["healthcheck"] = "true"
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

	// Compose creates a "default" network implicitly: services that name no
	// network join it, and it is never declared in the file. Any other network
	// that a service references without declaring it is also materialised, so
	// edges never point at a node that does not exist (which the store rejects
	// with a foreign key error, dropping the whole scan).
	networks := map[string]bool{}
	for name := range cf.Networks {
		networks[name] = true
	}
	for name, svc := range cf.Services {
		if len(svc.Networks.Names) == 0 {
			svc.Networks.Names = []string{"default"}
			cf.Services[name] = svc
		}
		for _, n := range svc.Networks.Names {
			networks[n] = true
		}
	}

	// Create network nodes
	for name := range networks {
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
				ID:     edgeID,
				FromID: fromID,
				ToID:   toID,
				Type:   models.EdgeDependsOn,
				Metadata: map[string]string{
					"via":       "depends_on",
					"raw_value": dep,
				},
			})
		}

		// network edges
		for _, net := range svc.Networks.Names {
			toID := "compose:network:" + net
			edgeID := fromID + "->connects_to->" + toID
			result.Edges = append(result.Edges, models.Edge{
				ID:     edgeID,
				FromID: fromID,
				ToID:   toID,
				Type:   models.EdgeConnectsTo,
				Metadata: map[string]string{
					"via":       "networks",
					"raw_value": net,
				},
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
			edgeID := fromID + "->mounts_volume->" + toID
			result.Edges = append(result.Edges, models.Edge{
				ID:     edgeID,
				FromID: fromID,
				ToID:   toID,
				Type:   models.EdgeMountsVolume,
				Metadata: map[string]string{
					"via":       "volumes",
					"raw_value": vol,
				},
			})
		}
	}

	return result
}

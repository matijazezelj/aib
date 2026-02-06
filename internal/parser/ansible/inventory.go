package ansible

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// hostEntry is the internal representation of an Ansible host.
type hostEntry struct {
	hostname   string
	groups     []string
	vars       map[string]string
	sourceFile string
}

// parseInventoryFile dispatches to INI or YAML parser based on file content/extension.
func parseInventoryFile(path string) ([]hostEntry, []string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".yml" || ext == ".yaml" {
		return parseYAMLInventory(path)
	}
	// For .ini or extensionless files, peek at content to detect YAML
	data, err := os.ReadFile(path) // #nosec G304 -- paths validated by SafeResolvePath
	if err != nil {
		return nil, nil, err
	}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "all:") {
		return parseYAMLInventoryBytes(data, path)
	}
	return parseINIInventory(path)
}

// --- INI inventory ---

// parseINIInventory parses a standard Ansible INI inventory file.
// Handles [group], host entries with inline vars, [group:vars], [group:children].
func parseINIInventory(path string) ([]hostEntry, []string, error) {
	f, err := os.Open(path) // #nosec G304 -- paths validated by SafeResolvePath
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var (
		hosts         []hostEntry
		warnings      []string
		currentGroup  string
		sectionType   string // "", "vars", "children"
		groupVars     = make(map[string]map[string]string)
		groupChildren = make(map[string][]string)
		groupHosts    = make(map[string][]string)
	)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Section header
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := line[1 : len(line)-1]
			if strings.Contains(section, ":vars") {
				currentGroup = strings.Split(section, ":")[0]
				sectionType = "vars"
			} else if strings.Contains(section, ":children") {
				currentGroup = strings.Split(section, ":")[0]
				sectionType = "children"
			} else {
				currentGroup = section
				sectionType = ""
			}
			continue
		}

		switch sectionType {
		case "vars":
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				if groupVars[currentGroup] == nil {
					groupVars[currentGroup] = make(map[string]string)
				}
				groupVars[currentGroup][strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		case "children":
			groupChildren[currentGroup] = append(groupChildren[currentGroup], line)
		default:
			host, err := parseINIHostLine(line, currentGroup, path)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skipping line in %s: %v", path, err))
				continue
			}
			hosts = append(hosts, host)
			groupHosts[currentGroup] = append(groupHosts[currentGroup], host.hostname)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, warnings, fmt.Errorf("reading %s: %w", path, err)
	}

	// Apply group vars to hosts in those groups
	for i := range hosts {
		for _, g := range hosts[i].groups {
			if gv, ok := groupVars[g]; ok {
				for k, v := range gv {
					if _, exists := hosts[i].vars[k]; !exists {
						hosts[i].vars[k] = v
					}
				}
			}
		}
	}

	// Resolve [group:children] — hosts in child groups also belong to parent
	resolveChildren(hosts, groupChildren, groupHosts)

	return hosts, warnings, nil
}

func parseINIHostLine(line, group, sourceFile string) (hostEntry, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return hostEntry{}, fmt.Errorf("empty host line")
	}

	h := hostEntry{
		hostname:   fields[0],
		vars:       make(map[string]string),
		sourceFile: sourceFile,
	}
	if group != "" {
		h.groups = []string{group}
	}

	for _, field := range fields[1:] {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) == 2 {
			h.vars[parts[0]] = parts[1]
		}
	}

	return h, nil
}

func resolveChildren(hosts []hostEntry, groupChildren, groupHosts map[string][]string) {
	hostIndex := make(map[string][]int)
	for i, h := range hosts {
		hostIndex[h.hostname] = append(hostIndex[h.hostname], i)
	}

	for parent, children := range groupChildren {
		for _, childGroup := range children {
			if childHostnames, ok := groupHosts[childGroup]; ok {
				for _, hostname := range childHostnames {
					for _, idx := range hostIndex[hostname] {
						hosts[idx].groups = append(hosts[idx].groups, parent)
					}
				}
			}
		}
	}
}

// --- YAML inventory ---

type yamlInventory struct {
	All yamlGroup `yaml:"all"`
}

type yamlGroup struct {
	Hosts    map[string]map[string]string `yaml:"hosts"`
	Children map[string]yamlGroup         `yaml:"children"`
	Vars     map[string]string            `yaml:"vars"`
}

func parseYAMLInventory(path string) ([]hostEntry, []string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- paths validated by SafeResolvePath
	if err != nil {
		return nil, nil, err
	}
	return parseYAMLInventoryBytes(data, path)
}

func parseYAMLInventoryBytes(data []byte, sourceFile string) ([]hostEntry, []string, error) {
	var inv yamlInventory
	if err := yaml.Unmarshal(data, &inv); err != nil {
		return nil, nil, fmt.Errorf("parsing YAML inventory: %w", err)
	}

	var hosts []hostEntry
	var warnings []string

	walkYAMLGroup(inv.All, []string{"all"}, inv.All.Vars, sourceFile, &hosts, &warnings)

	return hosts, warnings, nil
}

func walkYAMLGroup(group yamlGroup, groupPath []string, parentVars map[string]string, sourceFile string, hosts *[]hostEntry, warnings *[]string) {
	mergedVars := make(map[string]string)
	for k, v := range parentVars {
		mergedVars[k] = v
	}
	for k, v := range group.Vars {
		mergedVars[k] = v
	}

	for hostname, hostVars := range group.Hosts {
		h := hostEntry{
			hostname:   hostname,
			groups:     make([]string, len(groupPath)),
			vars:       make(map[string]string),
			sourceFile: sourceFile,
		}
		copy(h.groups, groupPath)

		for k, v := range mergedVars {
			h.vars[k] = v
		}
		for k, v := range hostVars {
			h.vars[k] = v
		}

		*hosts = append(*hosts, h)
	}

	for childName, childGroup := range group.Children {
		childPath := make([]string, len(groupPath)+1)
		copy(childPath, groupPath)
		childPath[len(groupPath)] = childName
		walkYAMLGroup(childGroup, childPath, mergedVars, sourceFile, hosts, warnings)
	}
}

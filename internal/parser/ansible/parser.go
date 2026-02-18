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

	depResult := inferHostDependencies(hostMap, hostnames, now)
	result.Nodes = append(result.Nodes, depResult.Nodes...)
	result.Edges = append(result.Edges, depResult.Edges...)
	result.Warnings = append(result.Warnings, depResult.Warnings...)

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

type depRule struct {
	edgeType  models.EdgeType
	targetFmt string // "host_or_node" or "k8s_service"
	assetHint string // e.g. "database"
}

var ansibleDependencyRules = map[string]depRule{
	"db_host":              {edgeType: models.EdgeDependsOn, targetFmt: "host_or_node", assetHint: "database"},
	"db_hosts":             {edgeType: models.EdgeDependsOn, targetFmt: "host_or_node", assetHint: "database"},
	"database_host":        {edgeType: models.EdgeDependsOn, targetFmt: "host_or_node", assetHint: "database"},
	"database_hosts":       {edgeType: models.EdgeDependsOn, targetFmt: "host_or_node", assetHint: "database"},
	"postgres_host":        {edgeType: models.EdgeDependsOn, targetFmt: "host_or_node", assetHint: "database"},
	"postgres_hosts":       {edgeType: models.EdgeDependsOn, targetFmt: "host_or_node", assetHint: "database"},
	"mysql_host":           {edgeType: models.EdgeDependsOn, targetFmt: "host_or_node", assetHint: "database"},
	"mysql_hosts":          {edgeType: models.EdgeDependsOn, targetFmt: "host_or_node", assetHint: "database"},
	"redis_host":           {edgeType: models.EdgeConnectsTo, targetFmt: "host_or_node"},
	"redis_hosts":          {edgeType: models.EdgeConnectsTo, targetFmt: "host_or_node"},
	"cache_host":           {edgeType: models.EdgeConnectsTo, targetFmt: "host_or_node"},
	"cache_hosts":          {edgeType: models.EdgeConnectsTo, targetFmt: "host_or_node"},
	"k8s_service":          {edgeType: models.EdgeConnectsTo, targetFmt: "k8s_service"},
	"k8s_services":         {edgeType: models.EdgeConnectsTo, targetFmt: "k8s_service"},
	"kubernetes_service":   {edgeType: models.EdgeConnectsTo, targetFmt: "k8s_service"},
	"kubernetes_services":  {edgeType: models.EdgeConnectsTo, targetFmt: "k8s_service"},
	"k8s_redis_service":    {edgeType: models.EdgeConnectsTo, targetFmt: "k8s_service"},
	"k8s_redis_services":   {edgeType: models.EdgeConnectsTo, targetFmt: "k8s_service"},
	"redis_k8s_service":    {edgeType: models.EdgeConnectsTo, targetFmt: "k8s_service"},
	"redis_k8s_services":   {edgeType: models.EdgeConnectsTo, targetFmt: "k8s_service"},
	"kubernetes_redis":     {edgeType: models.EdgeConnectsTo, targetFmt: "k8s_service"},
	"kubernetes_redis_svc": {edgeType: models.EdgeConnectsTo, targetFmt: "k8s_service"},
}

func inferHostDependencies(hostMap map[string]hostEntry, hostnames []string, now time.Time) *parser.ParseResult {
	result := &parser.ParseResult{}

	knownNodeIDs := make(map[string]bool)
	for _, hostname := range hostnames {
		knownNodeIDs[fmt.Sprintf("ansible:vm:%s", hostname)] = true
	}
	edgeSeen := make(map[string]bool)
	nodeSeen := make(map[string]bool)

	for _, hostname := range hostnames {
		h := hostMap[hostname]
		fromID := fmt.Sprintf("ansible:vm:%s", hostname)

		for key, raw := range h.vars {
			rule, ok := ansibleDependencyRules[strings.ToLower(strings.TrimSpace(key))]
			if !ok {
				continue
			}

			targets := splitDependencyTargets(raw)
			for _, target := range targets {
				var toID string
				isHostTarget := false
				switch rule.targetFmt {
				case "k8s_service":
					toID = normalizeK8sServiceID(target)
					if toID == "" {
						result.Warnings = append(result.Warnings, fmt.Sprintf("ansible dependency %s on %s has invalid k8s service target %q", hostname, key, target))
						continue
					}
					if !knownNodeIDs[toID] {
						nsName := strings.TrimPrefix(toID, "k8s:service:")
						nodeName := nsName
						if strings.Contains(nsName, "/") {
							nodeName = strings.SplitN(nsName, "/", 2)[1]
						}
						meta := map[string]string{
							"inferred": "true",
							"from_var": key,
						}
						if cs := inferK8sServiceConnectionString(key, toID, h); cs != "" {
							meta["connection_string"] = cs
						}
						result.Nodes = append(result.Nodes, models.Node{
							ID:         toID,
							Name:       nodeName,
							Type:       models.AssetService,
							Source:     "ansible",
							SourceFile: h.sourceFile,
							Provider:   "kubernetes",
							Metadata:   meta,
							LastSeen:  now,
							FirstSeen: now,
						})
						knownNodeIDs[toID] = true
					}
				default:
					if target == hostname {
						continue
					}
					if _, exists := hostMap[target]; exists {
						toID = fmt.Sprintf("ansible:vm:%s", target)
						isHostTarget = true
					} else if strings.Contains(target, ":") {
						toID = target
					} else {
						result.Warnings = append(result.Warnings, fmt.Sprintf("ansible dependency %s on %s references unknown target %q", hostname, key, target))
						continue
					}
				}

				if rule.assetHint == "database" {
					dbNodeID, dbNode := buildInferredDatabaseNode(h, key, target, toID, isHostTarget, hostMap, now)
					if dbNodeID != "" {
						toID = dbNodeID
						if !nodeSeen[dbNodeID] {
							result.Nodes = append(result.Nodes, dbNode)
							nodeSeen[dbNodeID] = true
							knownNodeIDs[dbNodeID] = true
						}

						if isHostTarget {
							hostNodeID := fmt.Sprintf("ansible:vm:%s", target)
							hostBindKey := fmt.Sprintf("%s|%s|%s", dbNodeID, models.EdgeConnectsTo, hostNodeID)
							if !edgeSeen[hostBindKey] {
								edgeSeen[hostBindKey] = true
								result.Edges = append(result.Edges, models.Edge{
									ID:     fmt.Sprintf("%s->%s->%s", dbNodeID, models.EdgeConnectsTo, hostNodeID),
									FromID: dbNodeID,
									ToID:   hostNodeID,
									Type:   models.EdgeConnectsTo,
									Metadata: map[string]string{
										"source": "ansible_inventory_var",
										"role":   "database_host",
									},
								})
							}
						}
					}
				}

				edgeKey := fmt.Sprintf("%s|%s|%s", fromID, rule.edgeType, toID)
				if edgeSeen[edgeKey] {
					continue
				}
				edgeSeen[edgeKey] = true

				result.Edges = append(result.Edges, models.Edge{
					ID:     fmt.Sprintf("%s->%s->%s", fromID, rule.edgeType, toID),
					FromID: fromID,
					ToID:   toID,
					Type:   rule.edgeType,
					Metadata: map[string]string{
						"source":   "ansible_inventory_var",
						"var":      key,
						"raw_value": target,
					},
				})
			}
		}
	}

	return result
}

func buildInferredDatabaseNode(h hostEntry, varKey, rawTarget, resolvedTarget string, isHostTarget bool, hostMap map[string]hostEntry, now time.Time) (string, models.Node) {
	dbName := firstNonEmpty(
		h.vars["db_name"],
		h.vars["database_name"],
		h.vars["postgres_db"],
		h.vars["mysql_database"],
		"database",
	)

	hostLabel := rawTarget
	hostAddress := rawTarget
	if isHostTarget {
		hostLabel = rawTarget
		hostAddress = rawTarget
		if targetHost, ok := hostMap[rawTarget]; ok {
			if ah := strings.TrimSpace(targetHost.vars["ansible_host"]); ah != "" {
				hostAddress = ah
			}
		}
	} else if strings.Contains(resolvedTarget, ":") {
		hostLabel = resolvedTarget
		hostAddress = resolvedTarget
	}

	// Choose sensible defaults based on which variable key triggered the inference.
	defaultPort := "5432"
	defaultScheme := "postgres"
	lowerKey := strings.ToLower(varKey)
	if strings.Contains(lowerKey, "mysql") {
		defaultPort = "3306"
		defaultScheme = "mysql"
	}

	dbPort := firstNonEmpty(h.vars["db_port"], h.vars["database_port"], h.vars["postgres_port"], h.vars["mysql_port"], defaultPort)
	dbScheme := firstNonEmpty(h.vars["db_scheme"], h.vars["database_scheme"], defaultScheme)

	connectionString := firstNonEmpty(h.vars["db_connection_string"], h.vars["database_url"], h.vars["connection_string"])
	if connectionString == "" {
		connectionString = fmt.Sprintf("%s://%s:%s/%s", dbScheme, hostAddress, dbPort, dbName)
	}

	dbNodeID := fmt.Sprintf("ansible:database:%s@%s", sanitizeNodePart(dbName), sanitizeNodePart(hostLabel))
	node := models.Node{
		ID:         dbNodeID,
		Name:       fmt.Sprintf("%s@%s", dbName, hostLabel),
		Type:       models.AssetDatabase,
		Source:     "ansible",
		SourceFile: h.sourceFile,
		Provider:   dbScheme,
		Metadata: map[string]string{
			"inferred":          "true",
			"from_var":          varKey,
			"db_name":           dbName,
			"db_host":           hostLabel,
			"db_port":           dbPort,
			"connection_string": connectionString,
		},
		LastSeen:  now,
		FirstSeen: now,
	}

	return dbNodeID, node
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func sanitizeNodePart(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	repl := strings.NewReplacer(" ", "-", "/", "_", ":", "_", "@", "_", "\\", "_", ".", "-")
	return repl.Replace(s)
}

func splitDependencyTargets(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		var out []string
		for _, p := range parts {
			v := strings.TrimSpace(p)
			if v != "" {
				out = append(out, v)
			}
		}
		return out
	}

	fields := strings.Fields(raw)
	if len(fields) > 1 {
		return fields
	}
	return []string{raw}
}

func normalizeK8sServiceID(target string) string {
	t := strings.TrimSpace(target)
	if t == "" {
		return ""
	}
	if strings.HasPrefix(t, "k8s:service:") {
		return t
	}
	if strings.Contains(t, "/") {
		return "k8s:service:" + t
	}
	return "k8s:service:default/" + t
}

func inferK8sServiceConnectionString(varKey, serviceID string, h hostEntry) string {
	key := strings.ToLower(strings.TrimSpace(varKey))
	if !strings.Contains(key, "redis") && !strings.Contains(strings.ToLower(serviceID), "redis") {
		return ""
	}

	port := firstNonEmpty(h.vars["redis_port"], h.vars["cache_port"], "6379")
	db := firstNonEmpty(h.vars["redis_db"], h.vars["cache_db"], "0")

	nsName := strings.TrimPrefix(serviceID, "k8s:service:")
	parts := strings.SplitN(nsName, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return ""
	}
	namespace := parts[0]
	service := parts[1]

	return fmt.Sprintf("redis://%s.%s.svc.cluster.local:%s/%s", service, namespace, port, db)
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

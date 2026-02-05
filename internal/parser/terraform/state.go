package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/matijazezelj/aib/internal/parser"
	"github.com/matijazezelj/aib/pkg/models"
)

// StateParser parses Terraform .tfstate files.
type StateParser struct{}

func NewStateParser() *StateParser {
	return &StateParser{}
}

func (p *StateParser) Name() string {
	return "terraform"
}

func (p *StateParser) Supported(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return strings.HasSuffix(path, ".tfstate")
	}
	// Check if directory contains .tfstate files
	matches, _ := filepath.Glob(filepath.Join(path, "*.tfstate"))
	return len(matches) > 0
}

func (p *StateParser) Parse(ctx context.Context, path string) (*parser.ParseResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	var stateFiles []string
	if info.IsDir() {
		matches, _ := filepath.Glob(filepath.Join(path, "*.tfstate"))
		stateFiles = matches
	} else {
		stateFiles = []string{path}
	}

	result := &parser.ParseResult{}
	for _, sf := range stateFiles {
		r, err := parseStateFile(sf)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to parse %s: %v", sf, err))
			continue
		}
		result.Nodes = append(result.Nodes, r.Nodes...)
		result.Edges = append(result.Edges, r.Edges...)
		result.Warnings = append(result.Warnings, r.Warnings...)
	}

	return result, nil
}

// tfState represents the top-level Terraform state file structure.
type tfState struct {
	Version   int          `json:"version"`
	Resources []tfResource `json:"resources"`
}

type tfResource struct {
	Type      string       `json:"type"`
	Name      string       `json:"name"`
	Provider  string       `json:"provider"`
	Module    string       `json:"module"`
	Mode      string       `json:"mode"`
	Instances []tfInstance `json:"instances"`
}

type tfInstance struct {
	Attributes    map[string]any `json:"attributes"`
	Dependencies  []string       `json:"dependencies"`
}

func parseStateFile(path string) (*parser.ParseResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	return parseStateBytes(data, path)
}

func mapResourceType(tfType string) models.AssetType {
	mapping := map[string]models.AssetType{
		// GCP
		"google_compute_instance":         models.AssetVM,
		"google_sql_database_instance":    models.AssetDatabase,
		"google_storage_bucket":           models.AssetBucket,
		"google_compute_network":          models.AssetNetwork,
		"google_compute_subnetwork":       models.AssetSubnet,
		"google_compute_address":          models.AssetIPAddress,
		"google_compute_global_address":   models.AssetIPAddress,
		"google_compute_firewall":         models.AssetFirewallRule,
		"google_dns_record_set":           models.AssetDNSRecord,
		"google_compute_forwarding_rule":  models.AssetLoadBalancer,
		"google_compute_target_pool":      models.AssetLoadBalancer,
		"google_container_cluster":        models.AssetNode,
		"google_container_node_pool":      models.AssetNode,
		"google_pubsub_topic":             models.AssetPubSub,
		"google_pubsub_subscription":      models.AssetQueue,
		"google_redis_instance":           models.AssetDatabase,
		// AWS
		"aws_instance":                    models.AssetVM,
		"aws_db_instance":                 models.AssetDatabase,
		"aws_rds_instance":                models.AssetDatabase,
		"aws_rds_cluster":                 models.AssetDatabase,
		"aws_s3_bucket":                   models.AssetBucket,
		"aws_vpc":                         models.AssetNetwork,
		"aws_subnet":                      models.AssetSubnet,
		"aws_eip":                         models.AssetIPAddress,
		"aws_security_group":              models.AssetFirewallRule,
		"aws_route53_record":              models.AssetDNSRecord,
		"aws_lb":                          models.AssetLoadBalancer,
		"aws_alb":                         models.AssetLoadBalancer,
		"aws_elb":                         models.AssetLoadBalancer,
		"aws_ecs_service":                 models.AssetService,
		"aws_ecs_task_definition":         models.AssetContainer,
		"aws_eks_cluster":                 models.AssetNode,
		"aws_sqs_queue":                   models.AssetQueue,
		"aws_sns_topic":                   models.AssetPubSub,
		"aws_elasticache_cluster":         models.AssetDatabase,
		// Azure
		"azurerm_virtual_machine":         models.AssetVM,
		"azurerm_linux_virtual_machine":   models.AssetVM,
		"azurerm_windows_virtual_machine": models.AssetVM,
		"azurerm_sql_server":              models.AssetDatabase,
		"azurerm_postgresql_server":       models.AssetDatabase,
		"azurerm_storage_account":         models.AssetBucket,
		"azurerm_virtual_network":         models.AssetNetwork,
		"azurerm_subnet":                  models.AssetSubnet,
		"azurerm_public_ip":               models.AssetIPAddress,
		"azurerm_network_security_group":  models.AssetFirewallRule,
		"azurerm_dns_a_record":            models.AssetDNSRecord,
		"azurerm_lb":                      models.AssetLoadBalancer,
		"azurerm_kubernetes_cluster":      models.AssetNode,
		// Cloudflare
		"cloudflare_record":               models.AssetDNSRecord,
		// TLS
		"tls_cert_request":                models.AssetCertificate,
		"tls_self_signed_cert":            models.AssetCertificate,
		"tls_locally_signed_cert":         models.AssetCertificate,
		"acme_certificate":                models.AssetCertificate,
		// Kubernetes (via TF provider)
		"kubernetes_namespace":            models.AssetNamespace,
		"kubernetes_service":              models.AssetService,
		"kubernetes_ingress":              models.AssetIngress,
		"kubernetes_secret":               models.AssetSecret,
		"kubernetes_deployment":           models.AssetPod,
	}

	if t, ok := mapping[tfType]; ok {
		return t
	}
	return ""
}

func extractProvider(providerRef string) string {
	// Provider ref looks like: provider["registry.terraform.io/hashicorp/google"]
	providerRef = strings.TrimPrefix(providerRef, `provider["`)
	providerRef = strings.TrimSuffix(providerRef, `"]`)

	parts := strings.Split(providerRef, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return providerRef
}

func extractMetadata(resourceType string, attrs map[string]any) map[string]string {
	meta := make(map[string]string)

	stringKeys := []string{
		"region", "zone", "location", "machine_type", "instance_type",
		"image", "ami", "arn", "self_link", "project",
		"network", "subnetwork", "ip_address", "private_ip",
		"public_ip", "network_ip", "nat_ip",
	}

	for _, key := range stringKeys {
		if v, ok := attrs[key].(string); ok && v != "" {
			meta[key] = v
		}
	}

	if tags, ok := attrs["tags"].(map[string]any); ok {
		for k, v := range tags {
			meta["tag:"+k] = fmt.Sprintf("%v", v)
		}
	}

	if labels, ok := attrs["labels"].(map[string]any); ok {
		for k, v := range labels {
			meta["label:"+k] = fmt.Sprintf("%v", v)
		}
	}

	meta["tf_type"] = resourceType

	return meta
}

func createAttributeEdges(nodeID string, resourceType string, attrs map[string]any, result *parser.ParseResult, refToNodeID map[string]string) {
	// Helper: try to resolve a resource path/name to a known node ID.
	resolveTarget := func(attrVal string, fallbackType models.AssetType) string {
		name := lastSegment(attrVal)
		// Check if any known node ID ends with this name.
		for _, nid := range refToNodeID {
			if strings.HasSuffix(nid, ":"+name) {
				return nid
			}
		}
		return fmt.Sprintf("tf:%s:%s", fallbackType, name)
	}

	// Network reference edges
	if network, ok := attrs["network"].(string); ok && network != "" {
		targetID := resolveTarget(network, models.AssetNetwork)
		result.Edges = append(result.Edges, models.Edge{
			ID:       fmt.Sprintf("%s->connects_to->%s", nodeID, targetID),
			FromID:   nodeID,
			ToID:     targetID,
			Type:     models.EdgeConnectsTo,
			Metadata: map[string]string{"via": "network"},
		})
	}

	// Subnetwork reference edges
	if subnet, ok := attrs["subnetwork"].(string); ok && subnet != "" {
		targetID := resolveTarget(subnet, models.AssetSubnet)
		result.Edges = append(result.Edges, models.Edge{
			ID:       fmt.Sprintf("%s->connects_to->%s", nodeID, targetID),
			FromID:   nodeID,
			ToID:     targetID,
			Type:     models.EdgeConnectsTo,
			Metadata: map[string]string{"via": "subnetwork"},
		})
	}

	// VPC/subnet ID references (AWS style)
	if vpcID, ok := attrs["vpc_id"].(string); ok && vpcID != "" {
		targetID := resolveTarget(vpcID, models.AssetNetwork)
		result.Edges = append(result.Edges, models.Edge{
			ID:       fmt.Sprintf("%s->connects_to->%s", nodeID, targetID),
			FromID:   nodeID,
			ToID:     targetID,
			Type:     models.EdgeConnectsTo,
			Metadata: map[string]string{"via": "vpc_id"},
		})
	}
}

func lastSegment(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

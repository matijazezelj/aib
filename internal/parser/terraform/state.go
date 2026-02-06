package terraform

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/matijazezelj/aib/internal/parser"
	"github.com/matijazezelj/aib/pkg/models"
)

// StateParser parses Terraform .tfstate files.
type StateParser struct{}

// NewStateParser creates a new Terraform state parser.
func NewStateParser() *StateParser {
	return &StateParser{}
}

// Name returns "terraform".
func (p *StateParser) Name() string {
	return "terraform"
}

// Supported returns true if the path is a .tfstate file or a directory containing one.
func (p *StateParser) Supported(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return strings.HasSuffix(path, ".tfstate")
	}
	// Check recursively for .tfstate files
	found := false
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(p, ".tfstate") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// Parse parses a single path (file or directory) for Terraform state.
func (p *StateParser) Parse(ctx context.Context, path string) (*parser.ParseResult, error) {
	return p.ParseMulti(ctx, []string{path})
}

// ParseMulti parses multiple paths (files or directories) with cross-state
// edge resolution across all paths. It builds a single global ref map from
// all state files before parsing, so edges between resources in different
// state files or directories resolve correctly.
func (p *StateParser) ParseMulti(ctx context.Context, paths []string) (*parser.ParseResult, error) {
	var stateFiles []string
	for _, path := range paths {
		resolved, err := parser.SafeResolvePath(path)
		if err != nil {
			return nil, err
		}

		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", resolved, err)
		}

		if info.IsDir() {
			_ = filepath.WalkDir(resolved, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if !d.IsDir() && strings.HasSuffix(p, ".tfstate") {
					stateFiles = append(stateFiles, p)
				}
				return nil
			})
		} else {
			stateFiles = append(stateFiles, resolved)
		}
	}

	result := &parser.ParseResult{}

	// Phase 1: read all files and build a global ref map across all state files.
	globalRefMap := make(map[string]string)
	stateData := make(map[string][]byte)
	for _, sf := range stateFiles {
		data, err := os.ReadFile(sf)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reading %s: %v", sf, err))
			continue
		}
		stateData[sf] = data
		refs, err := buildRefMap(data)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("building ref map for %s: %v", sf, err))
			continue
		}
		for k, v := range refs {
			globalRefMap[k] = v
		}
	}

	// Phase 2: parse each file using the global ref map for cross-state resolution.
	for _, sf := range stateFiles {
		data, ok := stateData[sf]
		if !ok {
			continue
		}
		r, err := parseStateBytesWithRefs(data, sf, globalRefMap)
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

// tfResource represents a single resource block in a Terraform state file.
type tfResource struct {
	Type      string       `json:"type"`
	Name      string       `json:"name"`
	Provider  string       `json:"provider"`
	Module    string       `json:"module"`
	Mode      string       `json:"mode"`
	Instances []tfInstance `json:"instances"`
}

// tfInstance represents a single instance of a Terraform resource.
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
		// GCP IAM
		"google_storage_bucket_iam_binding":   models.AssetIAMBinding,
		"google_storage_bucket_iam_policy":    models.AssetIAMPolicy,
		"google_storage_bucket_iam_member":    models.AssetIAMBinding,
		"google_project_iam_binding":          models.AssetIAMBinding,
		"google_project_iam_member":           models.AssetIAMBinding,
		"google_project_iam_policy":           models.AssetIAMPolicy,
		"google_service_account_iam_binding":  models.AssetIAMBinding,
		"google_service_account_iam_policy":   models.AssetIAMPolicy,
		"google_kms_crypto_key_iam_binding":   models.AssetIAMBinding,
		"google_kms_crypto_key_iam_policy":    models.AssetIAMPolicy,
		"google_kms_key_ring_iam_binding":     models.AssetIAMBinding,
		"google_kms_key_ring_iam_member":      models.AssetIAMBinding,
		// AWS IAM
		"aws_iam_role":                            models.AssetServiceAccount,
		"aws_iam_role_policy_attachment":           models.AssetIAMBinding,
		"aws_iam_policy":                           models.AssetIAMPolicy,
		"aws_iam_policy_attachment":                models.AssetIAMBinding,
		"aws_iam_user":                             models.AssetServiceAccount,
		"aws_iam_user_policy_attachment":           models.AssetIAMBinding,
		"aws_iam_user_group_membership":            models.AssetIAMBinding,
		"aws_iam_group":                            models.AssetIAMGroup,
		"aws_iam_group_membership":                 models.AssetIAMBinding,
		"aws_iam_group_policy_attachment":           models.AssetIAMBinding,
		// Azure IAM
		"azurerm_role_assignment":             models.AssetIAMBinding,
		// KMS
		"google_kms_key_ring":                models.AssetKMSKey,
		"google_kms_crypto_key":              models.AssetKMSKey,
		"aws_kms_key":                        models.AssetKMSKey,
		"azurerm_key_vault_key":              models.AssetKMSKey,
		// Service Accounts / Identity
		"google_service_account":             models.AssetServiceAccount,
		// CDN
		"aws_cloudfront_distribution":             models.AssetCDN,
		"aws_cloudfront_origin_access_identity":   models.AssetServiceAccount,
		"google_compute_backend_bucket":            models.AssetCDN,
		// Compute Disks
		"google_compute_disk":                models.AssetDisk,
		"aws_ebs_volume":                     models.AssetDisk,
		"azurerm_managed_disk":               models.AssetDisk,
		// Instance Groups / Auto-scaling
		"google_compute_instance_group":           models.AssetInstanceGroup,
		"google_compute_instance_group_manager":   models.AssetInstanceGroup,
		"aws_autoscaling_group":                   models.AssetInstanceGroup,
		// Health Checks / Backend Services
		"google_compute_health_check":             models.AssetHealthCheck,
		"google_compute_region_backend_service":    models.AssetBackendService,
		"google_compute_backend_service":           models.AssetBackendService,
		// S3 Bucket sub-resources (config of parent bucket)
		"aws_s3_bucket_acl":                       models.AssetIAMPolicy,
		"aws_s3_bucket_cors_configuration":         models.AssetBucket,
		"aws_s3_bucket_lifecycle_configuration":    models.AssetBucket,
		"aws_s3_bucket_logging":                    models.AssetBucket,
		"aws_s3_bucket_policy":                     models.AssetIAMPolicy,
		"aws_s3_bucket_versioning":                 models.AssetBucket,
		"aws_s3_bucket_ownership_controls":         models.AssetBucket,
		"aws_s3_bucket_replication_configuration":  models.AssetBucket,
		// Monitoring
		"pingdom_check":                      models.AssetMonitor,
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
	// Returns "" if the target node is not found in the current state.
	resolveTarget := func(attrVal string) string {
		name := lastSegment(attrVal)
		for _, nid := range refToNodeID {
			if strings.HasSuffix(nid, ":"+name) {
				return nid
			}
		}
		return ""
	}

	addEdge := func(targetID, via string) {
		if targetID == "" {
			return
		}
		result.Edges = append(result.Edges, models.Edge{
			ID:       fmt.Sprintf("%s->connects_to->%s", nodeID, targetID),
			FromID:   nodeID,
			ToID:     targetID,
			Type:     models.EdgeConnectsTo,
			Metadata: map[string]string{"via": via},
		})
	}

	// Network reference edges
	if network, ok := attrs["network"].(string); ok && network != "" {
		addEdge(resolveTarget(network), "network")
	}

	// Subnetwork reference edges
	if subnet, ok := attrs["subnetwork"].(string); ok && subnet != "" {
		addEdge(resolveTarget(subnet), "subnetwork")
	}

	// VPC/subnet ID references (AWS style)
	if vpcID, ok := attrs["vpc_id"].(string); ok && vpcID != "" {
		addEdge(resolveTarget(vpcID), "vpc_id")
	}
}

func lastSegment(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

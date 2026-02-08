package cloudformation

import (
	"context"
	"encoding/json"
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

// cfnTemplate represents a CloudFormation template.
type cfnTemplate struct {
	AWSTemplateFormatVersion string                 `json:"AWSTemplateFormatVersion" yaml:"AWSTemplateFormatVersion"`
	Resources                map[string]cfnResource `json:"Resources" yaml:"Resources"`
}

// cfnResource represents a single CloudFormation resource.
type cfnResource struct {
	Type       string         `json:"Type" yaml:"Type"`
	DependsOn  cfnDependsOn   `json:"DependsOn" yaml:"DependsOn"`
	Properties map[string]any `json:"Properties" yaml:"Properties"`
}

// cfnDependsOn handles both string and []string forms of DependsOn.
type cfnDependsOn struct {
	Resources []string
}

func (d *cfnDependsOn) UnmarshalJSON(data []byte) error {
	// Try string first
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		d.Resources = []string{single}
		return nil
	}
	// Try []string
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		d.Resources = list
		return nil
	}
	return fmt.Errorf("DependsOn must be string or []string")
}

func (d *cfnDependsOn) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		d.Resources = []string{node.Value}
		return nil
	case yaml.SequenceNode:
		return node.Decode(&d.Resources)
	default:
		return fmt.Errorf("DependsOn must be string or []string")
	}
}

// CFNParser parses AWS CloudFormation templates.
type CFNParser struct{}

// NewCFNParser creates a new CloudFormation parser.
func NewCFNParser() *CFNParser {
	return &CFNParser{}
}

// Name returns "cloudformation".
func (p *CFNParser) Name() string {
	return "cloudformation"
}

// Supported returns true if the path is a CloudFormation template.
func (p *CFNParser) Supported(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" && ext != ".json" {
		return false
	}

	data, err := os.ReadFile(path) // #nosec G304 -- paths validated by caller
	if err != nil {
		return false
	}

	// Quick content check for CFN markers
	content := string(data)
	return strings.Contains(content, "AWSTemplateFormatVersion") || strings.Contains(content, "\"Resources\"") || strings.Contains(content, "Resources:")
}

// Parse reads a CloudFormation template and returns discovered nodes and edges.
func (p *CFNParser) Parse(ctx context.Context, path string) (*parser.ParseResult, error) {
	return p.ParseMulti(ctx, []string{path})
}

// ParseMulti parses multiple CloudFormation templates with cross-file edge resolution.
func (p *CFNParser) ParseMulti(ctx context.Context, paths []string) (*parser.ParseResult, error) {
	result := &parser.ParseResult{}

	// Phase 1: build global ref map across all template files.
	globalRefMap := make(map[string]string)
	templateData := make(map[string][]byte)
	for _, path := range paths {
		resolved, err := parser.SafeResolvePath(path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("resolving %s: %v", path, err))
			continue
		}
		data, err := os.ReadFile(resolved) // #nosec G304 -- paths validated by SafeResolvePath
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reading %s: %v", resolved, err))
			continue
		}
		templateData[resolved] = data
		refs, err := buildCFNRefMap(data)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("building ref map for %s: %v", resolved, err))
			continue
		}
		for k, v := range refs {
			globalRefMap[k] = v
		}
	}

	// Phase 2: parse each template using the global ref map.
	for path, data := range templateData {
		r, err := parseCFNWithRefs(data, path, globalRefMap)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("parsing %s: %v", path, err))
			continue
		}
		result.Nodes = append(result.Nodes, r.Nodes...)
		result.Edges = append(result.Edges, r.Edges...)
		result.Warnings = append(result.Warnings, r.Warnings...)
	}

	return result, nil
}

// buildCFNRefMap builds a mapping from logical ID to node ID.
func buildCFNRefMap(data []byte) (map[string]string, error) {
	tmpl, err := unmarshalTemplate(data)
	if err != nil {
		return nil, err
	}

	refMap := make(map[string]string)
	for logicalID, res := range tmpl.Resources {
		assetType := mapCFNResourceType(res.Type)
		if assetType == "" {
			continue
		}
		refMap[logicalID] = fmt.Sprintf("cfn:%s:%s", assetType, logicalID)
	}
	return refMap, nil
}

// parseCFNWithRefs parses a CloudFormation template and creates nodes/edges.
func parseCFNWithRefs(data []byte, sourcePath string, refMap map[string]string) (*parser.ParseResult, error) {
	tmpl, err := unmarshalTemplate(data)
	if err != nil {
		return nil, err
	}

	result := &parser.ParseResult{}
	now := time.Now()

	// Sort logical IDs for deterministic output
	logicalIDs := make([]string, 0, len(tmpl.Resources))
	for id := range tmpl.Resources {
		logicalIDs = append(logicalIDs, id)
	}
	sort.Strings(logicalIDs)

	for _, logicalID := range logicalIDs {
		res := tmpl.Resources[logicalID]
		assetType := mapCFNResourceType(res.Type)
		if assetType == "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("unmapped CFN resource type: %s (%s)", res.Type, logicalID))
			continue
		}

		nodeID := fmt.Sprintf("cfn:%s:%s", assetType, logicalID)
		meta := extractCFNMetadata(res)
		meta["cfn_type"] = res.Type

		node := models.Node{
			ID:         nodeID,
			Name:       logicalID,
			Type:       assetType,
			Source:     "cloudformation",
			SourceFile: sourcePath,
			Provider:   "aws",
			Metadata:   meta,
			LastSeen:   now,
			FirstSeen:  now,
		}
		result.Nodes = append(result.Nodes, node)

		// Track edges as a set to avoid duplicates
		edgeSet := make(map[string]bool)

		// DependsOn edges
		for _, dep := range res.DependsOn.Resources {
			if targetID, ok := refMap[dep]; ok {
				key := fmt.Sprintf("%s->depends_on->%s", nodeID, targetID)
				if !edgeSet[key] {
					edgeSet[key] = true
					result.Edges = append(result.Edges, models.Edge{
						ID:       key,
						FromID:   nodeID,
						ToID:     targetID,
						Type:     models.EdgeDependsOn,
						Metadata: map[string]string{"via": "DependsOn"},
					})
				}
			}
		}

		// Ref and Fn::GetAtt edges from Properties
		refs := walkRefs(res.Properties, refMap)
		for _, ref := range refs {
			key := fmt.Sprintf("%s->depends_on->%s", nodeID, ref)
			if ref == nodeID {
				continue // skip self-references
			}
			if !edgeSet[key] {
				edgeSet[key] = true
				result.Edges = append(result.Edges, models.Edge{
					ID:       key,
					FromID:   nodeID,
					ToID:     ref,
					Type:     models.EdgeDependsOn,
					Metadata: map[string]string{"via": "Ref"},
				})
			}
		}

		// Property-based connects_to edges (VpcId, SubnetId, SecurityGroupIds)
		createPropertyEdges(nodeID, res.Properties, refMap, result, edgeSet)
	}

	return result, nil
}

// unmarshalTemplate tries JSON first, then YAML.
func unmarshalTemplate(data []byte) (*cfnTemplate, error) {
	var tmpl cfnTemplate
	if err := json.Unmarshal(data, &tmpl); err == nil && tmpl.Resources != nil {
		return &tmpl, nil
	}
	if err := yaml.Unmarshal(data, &tmpl); err == nil && tmpl.Resources != nil {
		return &tmpl, nil
	}
	return nil, fmt.Errorf("failed to parse as JSON or YAML CloudFormation template")
}

// walkRefs recursively walks a value looking for Ref and Fn::GetAtt intrinsic functions.
// Returns a deduplicated, sorted list of resolved node IDs.
func walkRefs(v any, refMap map[string]string) []string {
	seen := make(map[string]bool)
	walkRefsInto(v, refMap, seen)

	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func walkRefsInto(v any, refMap map[string]string, seen map[string]bool) {
	switch val := v.(type) {
	case map[string]any:
		// Check for Ref
		if ref, ok := val["Ref"].(string); ok {
			if !strings.HasPrefix(ref, "AWS::") {
				if nodeID, ok := refMap[ref]; ok {
					seen[nodeID] = true
				}
			}
			return
		}
		// Check for Fn::GetAtt
		if getAtt, ok := val["Fn::GetAtt"]; ok {
			if arr, ok := getAtt.([]any); ok && len(arr) >= 1 {
				if logicalID, ok := arr[0].(string); ok {
					if nodeID, ok := refMap[logicalID]; ok {
						seen[nodeID] = true
					}
				}
			}
			return
		}
		// Recurse into all values (including other Fn:: functions)
		for _, child := range val {
			walkRefsInto(child, refMap, seen)
		}
	case []any:
		for _, item := range val {
			walkRefsInto(item, refMap, seen)
		}
	}
}

// createPropertyEdges creates connects_to edges for known property references
// like VpcId, SubnetId, SecurityGroupIds that reference other logical IDs.
func createPropertyEdges(nodeID string, props map[string]any, refMap map[string]string, result *parser.ParseResult, edgeSet map[string]bool) {
	propKeys := []string{"VpcId", "SubnetId", "SubnetIds"}

	for _, key := range propKeys {
		val, ok := props[key]
		if !ok {
			continue
		}
		// Property might be a Ref (already handled), or a direct logical ID string
		if strVal, ok := val.(string); ok {
			if targetID, ok := refMap[strVal]; ok {
				edgeKey := fmt.Sprintf("%s->connects_to->%s", nodeID, targetID)
				if !edgeSet[edgeKey] {
					edgeSet[edgeKey] = true
					result.Edges = append(result.Edges, models.Edge{
						ID:       edgeKey,
						FromID:   nodeID,
						ToID:     targetID,
						Type:     models.EdgeConnectsTo,
						Metadata: map[string]string{"via": key},
					})
				}
			}
		}
	}

	// SecurityGroupIds is typically a list
	if sgIDs, ok := props["SecurityGroupIds"].([]any); ok {
		for _, sg := range sgIDs {
			if strVal, ok := sg.(string); ok {
				if targetID, ok := refMap[strVal]; ok {
					edgeKey := fmt.Sprintf("%s->connects_to->%s", nodeID, targetID)
					if !edgeSet[edgeKey] {
						edgeSet[edgeKey] = true
						result.Edges = append(result.Edges, models.Edge{
							ID:       edgeKey,
							FromID:   nodeID,
							ToID:     targetID,
							Type:     models.EdgeConnectsTo,
							Metadata: map[string]string{"via": "SecurityGroupIds"},
						})
					}
				}
			}
		}
	}
}

// extractCFNMetadata extracts useful metadata from CloudFormation resource properties.
func extractCFNMetadata(res cfnResource) map[string]string {
	meta := make(map[string]string)
	props := res.Properties

	stringKeys := []string{
		"InstanceType", "CidrBlock", "Engine", "Runtime", "BucketName",
		"FunctionName", "DBInstanceClass", "AvailabilityZone",
	}
	for _, key := range stringKeys {
		if v, ok := props[key].(string); ok && v != "" {
			meta[key] = v
		}
	}

	// CFN tags: [{Key: k, Value: v}, ...]
	if tags, ok := props["Tags"].([]any); ok {
		for _, t := range tags {
			if tag, ok := t.(map[string]any); ok {
				k, _ := tag["Key"].(string)
				v, _ := tag["Value"].(string)
				if k != "" {
					meta["tag:"+k] = v
				}
			}
		}
	}

	return meta
}

// mapCFNResourceType maps a CloudFormation resource type to an AssetType.
func mapCFNResourceType(cfnType string) models.AssetType {
	mapping := map[string]models.AssetType{
		// Compute
		"AWS::EC2::Instance":                             models.AssetVM,
		"AWS::Lambda::Function":                          models.AssetFunction,
		"AWS::ECS::Service":                              models.AssetService,
		"AWS::ECS::TaskDefinition":                       models.AssetContainer,
		"AWS::EKS::Cluster":                              models.AssetNode,
		"AWS::AutoScaling::AutoScalingGroup":              models.AssetInstanceGroup,
		"AWS::AutoScaling::LaunchConfiguration":           models.AssetVM,
		// Data
		"AWS::RDS::DBInstance":                            models.AssetDatabase,
		"AWS::RDS::DBCluster":                             models.AssetDatabase,
		"AWS::S3::Bucket":                                 models.AssetBucket,
		"AWS::DynamoDB::Table":                            models.AssetNoSQLDB,
		"AWS::ElastiCache::CacheCluster":                  models.AssetDatabase,
		"AWS::ElastiCache::ReplicationGroup":              models.AssetDatabase,
		// Network
		"AWS::EC2::VPC":                                   models.AssetNetwork,
		"AWS::EC2::Subnet":                                models.AssetSubnet,
		"AWS::EC2::SecurityGroup":                         models.AssetFirewallRule,
		"AWS::EC2::EIP":                                   models.AssetIPAddress,
		"AWS::ElasticLoadBalancingV2::LoadBalancer":        models.AssetLoadBalancer,
		"AWS::ElasticLoadBalancingV2::TargetGroup":         models.AssetLoadBalancer,
		"AWS::ElasticLoadBalancingV2::Listener":            models.AssetLoadBalancer,
		"AWS::EC2::InternetGateway":                        models.AssetNetwork,
		"AWS::EC2::NatGateway":                             models.AssetNetwork,
		"AWS::EC2::RouteTable":                             models.AssetNetwork,
		"AWS::EC2::SubnetRouteTableAssociation":            models.AssetNetwork,
		"AWS::EC2::VPCGatewayAttachment":                   models.AssetNetwork,
		// Security
		"AWS::IAM::Role":                                  models.AssetServiceAccount,
		"AWS::IAM::Policy":                                models.AssetIAMPolicy,
		"AWS::IAM::InstanceProfile":                       models.AssetServiceAccount,
		"AWS::IAM::User":                                  models.AssetServiceAccount,
		"AWS::IAM::Group":                                 models.AssetIAMGroup,
		"AWS::KMS::Key":                                   models.AssetKMSKey,
		"AWS::SecretsManager::Secret":                     models.AssetSecret,
		"AWS::CertificateManager::Certificate":            models.AssetCertificate,
		// Messaging
		"AWS::SQS::Queue":                                 models.AssetQueue,
		"AWS::SNS::Topic":                                 models.AssetPubSub,
		// DNS / CDN
		"AWS::Route53::RecordSet":                         models.AssetDNSRecord,
		"AWS::Route53::HostedZone":                        models.AssetDNSRecord,
		"AWS::CloudFront::Distribution":                   models.AssetCDN,
		// Serverless
		"AWS::ApiGateway::RestApi":                        models.AssetAPIGateway,
		"AWS::ApiGatewayV2::Api":                          models.AssetAPIGateway,
		// Storage
		"AWS::EBS::Volume":                                models.AssetDisk,
		"AWS::EFS::FileSystem":                            models.AssetDisk,
	}

	if t, ok := mapping[cfnType]; ok {
		return t
	}
	return ""
}

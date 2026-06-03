package pulumi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/matijazezelj/aib/internal/parser"
	"github.com/matijazezelj/aib/pkg/models"
)

// pulumiState represents the top-level Pulumi stack export structure.
type pulumiState struct {
	Version    int              `json:"version"`
	Deployment pulumiDeployment `json:"deployment"`
}

// pulumiDeployment wraps the resource list inside a deployment envelope.
type pulumiDeployment struct {
	Resources []pulumiResource `json:"resources"`
}

// pulumiResource represents a single resource in the Pulumi state.
type pulumiResource struct {
	URN          string         `json:"urn"`
	Type         string         `json:"type"`
	ID           string         `json:"id"`
	Custom       bool           `json:"custom"`
	Inputs       map[string]any `json:"inputs"`
	Outputs      map[string]any `json:"outputs"`
	Parent       string         `json:"parent"`
	Dependencies []string       `json:"dependencies"`
	Delete       bool           `json:"delete"`
}

// PulumiParser parses Pulumi stack export JSON files.
type PulumiParser struct{}

// NewPulumiParser creates a new Pulumi parser.
func NewPulumiParser() *PulumiParser {
	return &PulumiParser{}
}

// Supported returns true if the path is a Pulumi state JSON file.
func (p *PulumiParser) Supported(path string) bool {
	if !strings.HasSuffix(path, ".json") {
		return false
	}

	f, err := os.Open(path) // #nosec G304 -- paths validated by caller
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	// Check for Pulumi state markers in the header
	header := string(buf[:n])
	return strings.Contains(header, "\"deployment\"") && strings.Contains(header, "\"resources\"")
}

// Parse reads a Pulumi state file and returns discovered nodes and edges.
func (p *PulumiParser) Parse(ctx context.Context, path string) (*parser.ParseResult, error) {
	return p.ParseMulti(ctx, []string{path})
}

// ParseMulti parses multiple Pulumi state files with cross-file edge resolution.
func (p *PulumiParser) ParseMulti(ctx context.Context, paths []string) (*parser.ParseResult, error) {
	result := &parser.ParseResult{}

	// Phase 1: build global ref map across all state files.
	globalRefMap := make(map[string]string)
	stateData := make(map[string][]byte)
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
		stateData[resolved] = data
		refs, err := buildPulumiRefMap(data)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("building ref map for %s: %v", resolved, err))
			continue
		}
		for k, v := range refs {
			globalRefMap[k] = v
		}
	}

	// Phase 2: parse each file using the global ref map.
	// Sort paths for deterministic output.
	sortedPaths := make([]string, 0, len(stateData))
	for p := range stateData {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	for _, path := range sortedPaths {
		data := stateData[path]
		r, err := parsePulumiWithRefs(data, path, globalRefMap)
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

// buildPulumiRefMap builds a mapping from URN to node ID.
func buildPulumiRefMap(data []byte) (map[string]string, error) {
	var state pulumiState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshaling state: %w", err)
	}

	refMap := make(map[string]string)
	for _, res := range state.Deployment.Resources {
		if shouldSkipResource(res) {
			continue
		}
		assetType := mapPulumiResourceType(res.Type)
		if assetType == "" {
			continue
		}
		name := extractResourceName(res)
		refMap[res.URN] = fmt.Sprintf("plm:%s:%s", assetType, name)
	}
	return refMap, nil
}

// parsePulumiWithRefs parses a Pulumi state and creates nodes/edges.
func parsePulumiWithRefs(data []byte, sourcePath string, refMap map[string]string) (*parser.ParseResult, error) {
	var state pulumiState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshaling state: %w", err)
	}

	result := &parser.ParseResult{}
	now := time.Now()

	for _, res := range state.Deployment.Resources {
		if shouldSkipResource(res) {
			continue
		}

		assetType := mapPulumiResourceType(res.Type)
		if assetType == "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("unmapped Pulumi resource type: %s (%s)", res.Type, res.URN))
			continue
		}

		name := extractResourceName(res)
		nodeID := fmt.Sprintf("plm:%s:%s", assetType, name)
		provider := extractPulumiProvider(res.Type)
		meta := extractPulumiMetadata(res)

		node := models.Node{
			ID:         nodeID,
			Name:       name,
			Type:       assetType,
			Source:     "pulumi",
			SourceFile: sourcePath,
			Provider:   provider,
			Metadata:   meta,
			LastSeen:   now,
			FirstSeen:  now,
		}
		result.Nodes = append(result.Nodes, node)

		edgeSet := make(map[string]bool)

		// Dependency edges from dependencies array
		for _, depURN := range res.Dependencies {
			if targetID, ok := refMap[depURN]; ok {
				key := fmt.Sprintf("%s->depends_on->%s", nodeID, targetID)
				if !edgeSet[key] {
					edgeSet[key] = true
					result.Edges = append(result.Edges, models.Edge{
						ID:       key,
						FromID:   nodeID,
						ToID:     targetID,
						Type:     models.EdgeDependsOn,
						Metadata: map[string]string{"via": "dependencies"},
					})
				}
			}
		}

		// Attribute edges from inputs
		createPulumiAttributeEdges(nodeID, res.Inputs, refMap, result, edgeSet)

		// Parent edge
		if res.Parent != "" {
			if targetID, ok := refMap[res.Parent]; ok {
				key := fmt.Sprintf("%s->member_of->%s", nodeID, targetID)
				if !edgeSet[key] {
					edgeSet[key] = true
					result.Edges = append(result.Edges, models.Edge{
						ID:       key,
						FromID:   nodeID,
						ToID:     targetID,
						Type:     models.EdgeMemberOf,
						Metadata: map[string]string{"via": "parent"},
					})
				}
			}
		}
	}

	return result, nil
}

// shouldSkipResource returns true if the resource should be excluded from the graph.
func shouldSkipResource(res pulumiResource) bool {
	if res.Delete {
		return true
	}
	if res.Type == "pulumi:pulumi:Stack" {
		return true
	}
	if strings.HasPrefix(res.Type, "pulumi:providers:") {
		return true
	}
	return false
}

// extractResourceName extracts a human-readable name from the resource.
func extractResourceName(res pulumiResource) string {
	// Check outputs.name, then inputs.name
	if name, ok := res.Outputs["name"].(string); ok && name != "" {
		return name
	}
	if name, ok := res.Inputs["name"].(string); ok && name != "" {
		return name
	}
	// Fall back to last segment of URN after final "::"
	parts := strings.Split(res.URN, "::")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return res.URN
}

// extractPulumiProvider returns the provider from a Pulumi type string.
// e.g. "aws:ec2/instance:Instance" → "aws"
func extractPulumiProvider(pulumiType string) string {
	idx := strings.Index(pulumiType, ":")
	if idx > 0 {
		return pulumiType[:idx]
	}
	return pulumiType
}

// extractPulumiMetadata merges inputs + outputs (outputs take precedence)
// and extracts useful metadata fields.
func extractPulumiMetadata(res pulumiResource) map[string]string {
	meta := make(map[string]string)

	// Merge inputs and outputs; outputs override
	merged := make(map[string]any)
	for k, v := range res.Inputs {
		merged[k] = v
	}
	for k, v := range res.Outputs {
		merged[k] = v
	}

	stringKeys := []string{
		"region", "zone", "location", "availabilityZone",
		"instanceType", "machineType", "vmSize",
		"ami", "image", "arn", "selfLink", "project",
		"cidrBlock", "ipCidrRange",
		"engine", "engineVersion", "dbName", "acl",
	}
	for _, key := range stringKeys {
		if v, ok := merged[key].(string); ok && v != "" {
			meta[key] = v
		}
	}

	// Boolean security-relevant fields
	boolKeys := []string{
		"encrypted", "storageEncrypted", "publiclyAccessible",
		"deletionProtection", "multiAz",
	}
	for _, key := range boolKeys {
		switch v := merged[key].(type) {
		case bool:
			meta[key] = fmt.Sprintf("%t", v)
		case string:
			if v != "" {
				meta[key] = v
			}
		}
	}

	// Ingress CIDR blocks (for security groups)
	if rules, ok := merged["ingress"].([]any); ok {
		var cidrs []string
		for _, r := range rules {
			if rule, ok := r.(map[string]any); ok {
				if blocks, ok := rule["cidrBlocks"].([]any); ok {
					for _, b := range blocks {
						if s, ok := b.(string); ok {
							cidrs = append(cidrs, s)
						}
					}
				}
			}
		}
		if len(cidrs) > 0 {
			meta["ingress_cidrs"] = strings.Join(cidrs, ",")
		}
	}

	// Tags map
	if tags, ok := merged["tags"].(map[string]any); ok {
		for k, v := range tags {
			meta["tag:"+k] = fmt.Sprintf("%v", v)
		}
	}

	// Labels map
	if labels, ok := merged["labels"].(map[string]any); ok {
		for k, v := range labels {
			meta["label:"+k] = fmt.Sprintf("%v", v)
		}
	}

	meta["pulumi_type"] = res.Type

	return meta
}

// createPulumiAttributeEdges creates connects_to edges from known input keys.
func createPulumiAttributeEdges(nodeID string, inputs map[string]any, refMap map[string]string, result *parser.ParseResult, edgeSet map[string]bool) {
	// resolveByName tries to match a value to a known node ID by suffix matching.
	resolveByName := func(val string) string {
		for _, nid := range refMap {
			if strings.HasSuffix(nid, ":"+val) {
				return nid
			}
		}
		return ""
	}

	addEdge := func(targetID, via, rawValue string) {
		if targetID == "" || targetID == nodeID {
			return
		}
		key := fmt.Sprintf("%s->connects_to->%s", nodeID, targetID)
		if !edgeSet[key] {
			edgeSet[key] = true
			result.Edges = append(result.Edges, models.Edge{
				ID:       key,
				FromID:   nodeID,
				ToID:     targetID,
				Type:     models.EdgeConnectsTo,
				Metadata: map[string]string{"via": via, "raw_value": rawValue},
			})
		}
	}

	// Single-value references
	singleKeys := []string{
		"vpcId", "networkId", "network", "virtualNetworkName",
		"subnetId", "subnet", "subnetwork",
		"networkSecurityGroupId",
	}
	for _, key := range singleKeys {
		if val, ok := inputs[key].(string); ok && val != "" {
			addEdge(resolveByName(val), key, val)
		}
	}

	// Array-value references
	arrayKeys := []string{"subnetIds", "securityGroupIds", "securityGroups"}
	for _, key := range arrayKeys {
		if arr, ok := inputs[key].([]any); ok {
			for _, item := range arr {
				if val, ok := item.(string); ok && val != "" {
					addEdge(resolveByName(val), key, val)
				}
			}
		}
	}
}

// mapPulumiResourceType maps a Pulumi resource type to an AssetType.
func mapPulumiResourceType(pulumiType string) models.AssetType {
	mapping := map[string]models.AssetType{
		// AWS EC2
		"aws:ec2/instance:Instance":                       models.AssetVM,
		"aws:ec2/vpc:Vpc":                                 models.AssetNetwork,
		"aws:ec2/subnet:Subnet":                           models.AssetSubnet,
		"aws:ec2/securityGroup:SecurityGroup":              models.AssetFirewallRule,
		"aws:ec2/eip:Eip":                                 models.AssetIPAddress,
		"aws:ec2/internetGateway:InternetGateway":          models.AssetNetwork,
		"aws:ec2/natGateway:NatGateway":                    models.AssetNetwork,
		"aws:ec2/routeTable:RouteTable":                    models.AssetNetwork,
		"aws:ec2/routeTableAssociation:RouteTableAssociation": models.AssetNetwork,
		"aws:ec2/volume:Volume":                            models.AssetDisk,
		// AWS S3
		"aws:s3/bucket:Bucket":                            models.AssetBucket,
		"aws:s3/bucketV2:BucketV2":                        models.AssetBucket,
		"aws:s3/bucketPolicy:BucketPolicy":                models.AssetIAMPolicy,
		// AWS RDS
		"aws:rds/instance:Instance":                       models.AssetDatabase,
		"aws:rds/cluster:Cluster":                         models.AssetDatabase,
		// AWS DynamoDB
		"aws:dynamodb/table:Table":                        models.AssetNoSQLDB,
		// AWS Lambda
		"aws:lambda/function:Function":                    models.AssetFunction,
		// AWS Route53
		"aws:route53/record:Record":                       models.AssetDNSRecord,
		"aws:route53/zone:Zone":                           models.AssetDNSRecord,
		// AWS CloudFront
		"aws:cloudfront/distribution:Distribution":         models.AssetCDN,
		// AWS IAM
		"aws:iam/role:Role":                               models.AssetServiceAccount,
		"aws:iam/user:User":                               models.AssetServiceAccount,
		"aws:iam/policy:Policy":                           models.AssetIAMPolicy,
		"aws:iam/group:Group":                             models.AssetIAMGroup,
		"aws:iam/rolePolicyAttachment:RolePolicyAttachment": models.AssetIAMBinding,
		"aws:iam/userPolicyAttachment:UserPolicyAttachment": models.AssetIAMBinding,
		"aws:iam/groupPolicyAttachment:GroupPolicyAttachment": models.AssetIAMBinding,
		"aws:iam/instanceProfile:InstanceProfile":         models.AssetServiceAccount,
		// AWS KMS
		"aws:kms/key:Key":                                 models.AssetKMSKey,
		// AWS Secrets Manager
		"aws:secretsmanager/secret:Secret":                models.AssetSecret,
		// AWS SQS / SNS
		"aws:sqs/queue:Queue":                             models.AssetQueue,
		"aws:sns/topic:Topic":                             models.AssetPubSub,
		// AWS ACM
		"aws:acm/certificate:Certificate":                 models.AssetCertificate,
		// AWS API Gateway
		"aws:apigateway/restApi:RestApi":                  models.AssetAPIGateway,
		"aws:apigatewayv2/api:Api":                        models.AssetAPIGateway,
		// AWS ECS
		"aws:ecs/service:Service":                         models.AssetService,
		"aws:ecs/taskDefinition:TaskDefinition":           models.AssetContainer,
		"aws:ecs/cluster:Cluster":                         models.AssetNode,
		// AWS EKS
		"aws:eks/cluster:Cluster":                         models.AssetNode,
		// AWS ALB / NLB
		"aws:lb/loadBalancer:LoadBalancer":                models.AssetLoadBalancer,
		"aws:alb/loadBalancer:LoadBalancer":               models.AssetLoadBalancer,
		"aws:lb/targetGroup:TargetGroup":                  models.AssetLoadBalancer,
		"aws:lb/listener:Listener":                        models.AssetLoadBalancer,
		// AWS ElastiCache
		"aws:elasticache/cluster:Cluster":                 models.AssetDatabase,
		"aws:elasticache/replicationGroup:ReplicationGroup": models.AssetDatabase,
		// AWS Auto Scaling
		"aws:autoscaling/group:Group":                     models.AssetInstanceGroup,

		// GCP Compute
		"gcp:compute/instance:Instance":                   models.AssetVM,
		"gcp:compute/network:Network":                     models.AssetNetwork,
		"gcp:compute/subnetwork:Subnetwork":               models.AssetSubnet,
		"gcp:compute/firewall:Firewall":                   models.AssetFirewallRule,
		"gcp:compute/disk:Disk":                           models.AssetDisk,
		"gcp:compute/address:Address":                     models.AssetIPAddress,
		"gcp:compute/globalAddress:GlobalAddress":          models.AssetIPAddress,
		"gcp:compute/forwardingRule:ForwardingRule":        models.AssetLoadBalancer,
		"gcp:compute/targetPool:TargetPool":               models.AssetLoadBalancer,
		"gcp:compute/healthCheck:HealthCheck":              models.AssetHealthCheck,
		"gcp:compute/backendService:BackendService":        models.AssetBackendService,
		"gcp:compute/regionBackendService:RegionBackendService": models.AssetBackendService,
		"gcp:compute/backendBucket:BackendBucket":          models.AssetCDN,
		"gcp:compute/instanceGroup:InstanceGroup":          models.AssetInstanceGroup,
		"gcp:compute/instanceGroupManager:InstanceGroupManager": models.AssetInstanceGroup,
		// GCP Storage
		"gcp:storage/bucket:Bucket":                       models.AssetBucket,
		// GCP SQL
		"gcp:sql/databaseInstance:DatabaseInstance":        models.AssetDatabase,
		// GCP Redis
		"gcp:redis/instance:Instance":                     models.AssetDatabase,
		// GCP BigQuery
		"gcp:bigquery/dataset:Dataset":                    models.AssetDatabase,
		"gcp:bigquery/table:Table":                        models.AssetDatabase,
		// GCP DNS
		"gcp:dns/recordSet:RecordSet":                     models.AssetDNSRecord,
		// GCP Cloud Functions
		"gcp:cloudfunctions/function:Function":             models.AssetFunction,
		"gcp:cloudfunctionsv2/function:Function":           models.AssetFunction,
		// GCP Cloud Run
		"gcp:cloudrun/service:Service":                    models.AssetService,
		"gcp:cloudrunv2/service:Service":                  models.AssetService,
		// GCP IAM
		"gcp:serviceAccount/account:Account":              models.AssetServiceAccount,
		"gcp:projects/iAMBinding:IAMBinding":              models.AssetIAMBinding,
		"gcp:projects/iAMMember:IAMMember":                models.AssetIAMBinding,
		"gcp:projects/iAMPolicy:IAMPolicy":                models.AssetIAMPolicy,
		// GCP KMS
		"gcp:kms/keyRing:KeyRing":                         models.AssetKMSKey,
		"gcp:kms/cryptoKey:CryptoKey":                     models.AssetKMSKey,
		// GCP Secret Manager
		"gcp:secretmanager/secret:Secret":                 models.AssetSecret,
		// GCP Pub/Sub
		"gcp:pubsub/topic:Topic":                          models.AssetPubSub,
		"gcp:pubsub/subscription:Subscription":            models.AssetQueue,
		// GCP Container
		"gcp:container/cluster:Cluster":                   models.AssetNode,
		"gcp:container/nodePool:NodePool":                 models.AssetNode,

		// Azure Native
		"azure-native:compute:VirtualMachine":             models.AssetVM,
		"azure-native:network:VirtualNetwork":             models.AssetNetwork,
		"azure-native:network:Subnet":                     models.AssetSubnet,
		"azure-native:network:NetworkSecurityGroup":        models.AssetFirewallRule,
		"azure-native:network:LoadBalancer":                models.AssetLoadBalancer,
		"azure-native:network:PublicIPAddress":             models.AssetIPAddress,
		"azure-native:storage:StorageAccount":              models.AssetBucket,
		"azure-native:sql:Server":                         models.AssetDatabase,
		"azure-native:dbforpostgresql:Server":             models.AssetDatabase,
		"azure-native:dbformysql:Server":                  models.AssetDatabase,
		"azure-native:keyvault:Vault":                     models.AssetKMSKey,
		"azure-native:cdn:Endpoint":                       models.AssetCDN,
		"azure-native:containerservice:ManagedCluster":    models.AssetNode,
		"azure-native:authorization:RoleAssignment":       models.AssetIAMBinding,
		// Azure Classic
		"azure:compute/virtualMachine:VirtualMachine":     models.AssetVM,
		"azure:network/virtualNetwork:VirtualNetwork":     models.AssetNetwork,
		"azure:network/subnet:Subnet":                     models.AssetSubnet,
		"azure:network/networkSecurityGroup:NetworkSecurityGroup": models.AssetFirewallRule,
		"azure:lb/loadBalancer:LoadBalancer":               models.AssetLoadBalancer,
		"azure:network/publicIp:PublicIp":                  models.AssetIPAddress,
		"azure:storage/account:Account":                    models.AssetBucket,
		"azure:sql/server:Server":                          models.AssetDatabase,
		"azure:postgresql/server:Server":                   models.AssetDatabase,
		"azure:keyvault/keyVault:KeyVault":                 models.AssetKMSKey,
		"azure:containerservice/kubernetesCluster:KubernetesCluster": models.AssetNode,

		// Kubernetes
		"kubernetes:core/v1:Namespace":                    models.AssetNamespace,
		"kubernetes:core/v1:Service":                      models.AssetService,
		"kubernetes:core/v1:Secret":                       models.AssetSecret,
		"kubernetes:apps/v1:Deployment":                   models.AssetPod,
		"kubernetes:networking.k8s.io/v1:Ingress":         models.AssetIngress,

		// TLS
		"tls:index/selfSignedCert:SelfSignedCert":         models.AssetCertificate,
		"tls:index/locallySignedCert:LocallySignedCert":   models.AssetCertificate,
	}

	if t, ok := mapping[pulumiType]; ok {
		return t
	}
	return ""
}

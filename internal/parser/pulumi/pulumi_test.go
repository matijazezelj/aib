package pulumi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/matijazezelj/aib/pkg/models"
)


func TestPulumiParser_Supported(t *testing.T) {
	p := NewPulumiParser()

	t.Run("valid pulumi state", func(t *testing.T) {
		path := filepath.Join("testdata", "simple.json")
		if !p.Supported(path) {
			t.Error("Supported() = false for valid Pulumi state file")
		}
	})

	t.Run("non-json file", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "test.yaml")
		_ = os.WriteFile(tmp, []byte("foo: bar"), 0o644)
		if p.Supported(tmp) {
			t.Error("Supported() = true for YAML file")
		}
	})

	t.Run("json but not pulumi", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "test.json")
		_ = os.WriteFile(tmp, []byte(`{"version": 4, "resources": []}`), 0o644)
		if p.Supported(tmp) {
			t.Error("Supported() = true for Terraform-style JSON")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		if p.Supported("/nonexistent/file.json") {
			t.Error("Supported() = true for nonexistent file")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(tmp, []byte(`{invalid`), 0o644)
		if p.Supported(tmp) {
			t.Error("Supported() = true for invalid JSON")
		}
	})
}

func TestParsePulumi_Simple(t *testing.T) {
	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "simple.json"))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if len(result.Nodes) != 5 {
		t.Fatalf("got %d nodes, want 5", len(result.Nodes))
	}

	// Build node map for easier assertions
	nodeMap := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeMap[n.ID] = n
	}

	// Verify VPC
	vpc, ok := nodeMap["plm:network:main-vpc"]
	if !ok {
		t.Fatal("missing VPC node plm:network:main-vpc")
	}
	if vpc.Type != models.AssetNetwork {
		t.Errorf("vpc type = %q, want %q", vpc.Type, models.AssetNetwork)
	}
	if vpc.Source != "pulumi" {
		t.Errorf("vpc source = %q, want %q", vpc.Source, "pulumi")
	}
	if vpc.Provider != "aws" {
		t.Errorf("vpc provider = %q, want %q", vpc.Provider, "aws")
	}
	if vpc.Name != "main-vpc" {
		t.Errorf("vpc name = %q, want %q", vpc.Name, "main-vpc")
	}

	// Verify subnet
	subnet, ok := nodeMap["plm:subnet:public-subnet"]
	if !ok {
		t.Fatal("missing subnet node plm:subnet:public-subnet")
	}
	if subnet.Type != models.AssetSubnet {
		t.Errorf("subnet type = %q, want %q", subnet.Type, models.AssetSubnet)
	}

	// Verify SG
	sg, ok := nodeMap["plm:firewall_rule:web-sg"]
	if !ok {
		t.Fatal("missing SG node plm:firewall_rule:web-sg")
	}
	if sg.Type != models.AssetFirewallRule {
		t.Errorf("sg type = %q, want %q", sg.Type, models.AssetFirewallRule)
	}

	// Verify instance
	instance, ok := nodeMap["plm:vm:web-server"]
	if !ok {
		t.Fatal("missing instance node plm:vm:web-server")
	}
	if instance.Type != models.AssetVM {
		t.Errorf("instance type = %q, want %q", instance.Type, models.AssetVM)
	}

	// Verify bucket (no inputs.name / outputs.name → falls back to URN segment)
	bucket, ok := nodeMap["plm:bucket:data-bucket"]
	if !ok {
		t.Fatal("missing bucket node plm:bucket:data-bucket")
	}
	if bucket.Type != models.AssetBucket {
		t.Errorf("bucket type = %q, want %q", bucket.Type, models.AssetBucket)
	}
}

func TestParsePulumi_Edges(t *testing.T) {
	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "simple.json"))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	// Build edge map keyed by type+from+to
	type edgeKey struct{ typ, from, to string }
	edgeMap := make(map[edgeKey]models.Edge)
	for _, e := range result.Edges {
		edgeMap[edgeKey{string(e.Type), e.FromID, e.ToID}] = e
	}

	// subnet depends_on VPC
	if _, ok := edgeMap[edgeKey{"depends_on", "plm:subnet:public-subnet", "plm:network:main-vpc"}]; !ok {
		t.Error("missing depends_on edge: subnet → VPC")
	}

	// SG depends_on VPC
	if _, ok := edgeMap[edgeKey{"depends_on", "plm:firewall_rule:web-sg", "plm:network:main-vpc"}]; !ok {
		t.Error("missing depends_on edge: SG → VPC")
	}

	// instance depends_on subnet
	if _, ok := edgeMap[edgeKey{"depends_on", "plm:vm:web-server", "plm:subnet:public-subnet"}]; !ok {
		t.Error("missing depends_on edge: instance → subnet")
	}

	// instance depends_on SG
	if _, ok := edgeMap[edgeKey{"depends_on", "plm:vm:web-server", "plm:firewall_rule:web-sg"}]; !ok {
		t.Error("missing depends_on edge: instance → SG")
	}
}

func TestParsePulumi_AttributeEdges(t *testing.T) {
	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "simple.json"))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	type edgeKey struct{ typ, from, to string }
	edgeMap := make(map[edgeKey]models.Edge)
	for _, e := range result.Edges {
		edgeMap[edgeKey{string(e.Type), e.FromID, e.ToID}] = e
	}

	// subnet connects_to VPC via vpcId
	if _, ok := edgeMap[edgeKey{"connects_to", "plm:subnet:public-subnet", "plm:network:main-vpc"}]; !ok {
		t.Error("missing connects_to edge: subnet → VPC (vpcId)")
	}

	// SG connects_to VPC via vpcId
	if _, ok := edgeMap[edgeKey{"connects_to", "plm:firewall_rule:web-sg", "plm:network:main-vpc"}]; !ok {
		t.Error("missing connects_to edge: SG → VPC (vpcId)")
	}

	// instance connects_to subnet via subnetId
	if _, ok := edgeMap[edgeKey{"connects_to", "plm:vm:web-server", "plm:subnet:public-subnet"}]; !ok {
		t.Error("missing connects_to edge: instance → subnet (subnetId)")
	}

	// instance connects_to SG via securityGroupIds
	if _, ok := edgeMap[edgeKey{"connects_to", "plm:vm:web-server", "plm:firewall_rule:web-sg"}]; !ok {
		t.Error("missing connects_to edge: instance → SG (securityGroupIds)")
	}
}

func TestParsePulumi_EdgeMetadata(t *testing.T) {
	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "simple.json"))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	type edgeKey struct{ typ, from, to string }
	edgeMap := make(map[edgeKey]models.Edge)
	for _, e := range result.Edges {
		edgeMap[edgeKey{string(e.Type), e.FromID, e.ToID}] = e
	}

	// depends_on edge should have via=dependencies
	e := edgeMap[edgeKey{"depends_on", "plm:subnet:public-subnet", "plm:network:main-vpc"}]
	if e.Metadata["via"] != "dependencies" {
		t.Errorf("depends_on edge via = %q, want \"dependencies\"", e.Metadata["via"])
	}

	// connects_to attribute edge should have via and raw_value
	e = edgeMap[edgeKey{"connects_to", "plm:subnet:public-subnet", "plm:network:main-vpc"}]
	if e.Metadata["via"] == "" {
		t.Error("connects_to edge via should not be empty")
	}
	if e.Metadata["raw_value"] == "" {
		t.Error("connects_to edge raw_value should not be empty")
	}
}

func TestParsePulumi_Metadata(t *testing.T) {
	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "simple.json"))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	nodeMap := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeMap[n.ID] = n
	}

	// VPC tags
	vpc := nodeMap["plm:network:main-vpc"]
	if vpc.Metadata["tag:Name"] != "main-vpc" {
		t.Errorf("vpc tag:Name = %q, want %q", vpc.Metadata["tag:Name"], "main-vpc")
	}
	if vpc.Metadata["tag:env"] != "dev" {
		t.Errorf("vpc tag:env = %q, want %q", vpc.Metadata["tag:env"], "dev")
	}
	if vpc.Metadata["cidrBlock"] != "10.0.0.0/16" {
		t.Errorf("vpc cidrBlock = %q, want %q", vpc.Metadata["cidrBlock"], "10.0.0.0/16")
	}
	if vpc.Metadata["pulumi_type"] != "aws:ec2/vpc:Vpc" {
		t.Errorf("vpc pulumi_type = %q, want %q", vpc.Metadata["pulumi_type"], "aws:ec2/vpc:Vpc")
	}

	// Instance metadata
	instance := nodeMap["plm:vm:web-server"]
	if instance.Metadata["instanceType"] != "t3.medium" {
		t.Errorf("instance instanceType = %q, want %q", instance.Metadata["instanceType"], "t3.medium")
	}
	if instance.Metadata["ami"] != "ami-0123456789abcdef0" {
		t.Errorf("instance ami = %q, want %q", instance.Metadata["ami"], "ami-0123456789abcdef0")
	}

	// Bucket metadata (outputs override inputs)
	bucket := nodeMap["plm:bucket:data-bucket"]
	if bucket.Metadata["arn"] != "arn:aws:s3:::data-bucket-abc123" {
		t.Errorf("bucket arn = %q, want %q", bucket.Metadata["arn"], "arn:aws:s3:::data-bucket-abc123")
	}
	if bucket.Metadata["region"] != "us-east-1" {
		t.Errorf("bucket region = %q, want %q", bucket.Metadata["region"], "us-east-1")
	}
}

func TestParsePulumi_SkipsProviderAndStack(t *testing.T) {
	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "simple.json"))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	for _, n := range result.Nodes {
		if n.Type == "" {
			t.Errorf("node %q has empty type (stack/provider leaked)", n.ID)
		}
		if n.ID == "pulumi:pulumi:Stack" || n.Provider == "pulumi" {
			t.Errorf("node %q appears to be a stack or provider resource", n.ID)
		}
	}
}

func TestParsePulumi_SkipsDeleted(t *testing.T) {
	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "full_stack.json"))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	for _, n := range result.Nodes {
		if n.Name == "deleted-server" || n.ID == "plm:vm:deleted-server" {
			t.Error("deleted resource should not appear in nodes")
		}
	}
}

func TestParsePulumi_UnmappedType(t *testing.T) {
	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "full_stack.json"))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	foundWarning := false
	for _, w := range result.Warnings {
		if contains(w, "unmapped Pulumi resource type") && contains(w, "custom:MyCustomResource") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected warning about unmapped type custom:MyCustomResource")
	}
}

func TestParsePulumi_FullStack(t *testing.T) {
	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "full_stack.json"))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	// 19 total resources minus 1 stack, 2 providers, 1 deleted, 1 unmapped = 14
	if len(result.Nodes) != 14 {
		t.Errorf("got %d nodes, want 14", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s)", n.ID, n.Type)
		}
	}

	// Verify multi-cloud providers
	providers := make(map[string]bool)
	for _, n := range result.Nodes {
		providers[n.Provider] = true
	}
	for _, p := range []string{"aws", "gcp", "azure-native"} {
		if !providers[p] {
			t.Errorf("missing provider %q", p)
		}
	}

	// Verify GCP-specific metadata
	nodeMap := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeMap[n.ID] = n
	}
	worker := nodeMap["plm:vm:gcp-worker"]
	if worker.Metadata["machineType"] != "n1-standard-4" {
		t.Errorf("gcp-worker machineType = %q, want %q", worker.Metadata["machineType"], "n1-standard-4")
	}
	if worker.Metadata["label:team"] != "backend" {
		t.Errorf("gcp-worker label:team = %q, want %q", worker.Metadata["label:team"], "backend")
	}

	// Verify Azure-specific node
	if _, ok := nodeMap["plm:vm:azure-vm"]; !ok {
		t.Error("missing Azure VM node plm:vm:azure-vm")
	}
}

func TestParsePulumi_CrossFileEdges(t *testing.T) {
	p := NewPulumiParser()
	result, err := p.ParseMulti(context.Background(), []string{
		filepath.Join("testdata", "simple.json"),
		filepath.Join("testdata", "cross_file.json"),
	})
	if err != nil {
		t.Fatalf("ParseMulti() error: %v", err)
	}

	type edgeKey struct{ typ, from, to string }
	edgeMap := make(map[edgeKey]bool)
	for _, e := range result.Edges {
		edgeMap[edgeKey{string(e.Type), e.FromID, e.ToID}] = true
	}

	// app-server depends_on public-subnet from simple.json
	if !edgeMap[edgeKey{"depends_on", "plm:vm:app-server", "plm:subnet:public-subnet"}] {
		t.Error("missing cross-file depends_on edge: app-server → public-subnet")
	}

	// app-server depends_on web-sg from simple.json
	if !edgeMap[edgeKey{"depends_on", "plm:vm:app-server", "plm:firewall_rule:web-sg"}] {
		t.Error("missing cross-file depends_on edge: app-server → web-sg")
	}

	// handler depends_on data-bucket from simple.json
	if !edgeMap[edgeKey{"depends_on", "plm:function:handler", "plm:bucket:data-bucket"}] {
		t.Error("missing cross-file depends_on edge: handler → data-bucket")
	}
}

func TestMapPulumiResourceType(t *testing.T) {
	tests := []struct {
		input string
		want  models.AssetType
	}{
		// AWS
		{"aws:ec2/instance:Instance", models.AssetVM},
		{"aws:ec2/vpc:Vpc", models.AssetNetwork},
		{"aws:ec2/subnet:Subnet", models.AssetSubnet},
		{"aws:ec2/securityGroup:SecurityGroup", models.AssetFirewallRule},
		{"aws:ec2/eip:Eip", models.AssetIPAddress},
		{"aws:ec2/volume:Volume", models.AssetDisk},
		{"aws:s3/bucket:Bucket", models.AssetBucket},
		{"aws:s3/bucketV2:BucketV2", models.AssetBucket},
		{"aws:s3/bucketPolicy:BucketPolicy", models.AssetIAMPolicy},
		{"aws:rds/instance:Instance", models.AssetDatabase},
		{"aws:rds/cluster:Cluster", models.AssetDatabase},
		{"aws:dynamodb/table:Table", models.AssetNoSQLDB},
		{"aws:lambda/function:Function", models.AssetFunction},
		{"aws:route53/record:Record", models.AssetDNSRecord},
		{"aws:route53/zone:Zone", models.AssetDNSRecord},
		{"aws:cloudfront/distribution:Distribution", models.AssetCDN},
		{"aws:iam/role:Role", models.AssetServiceAccount},
		{"aws:iam/user:User", models.AssetServiceAccount},
		{"aws:iam/policy:Policy", models.AssetIAMPolicy},
		{"aws:iam/group:Group", models.AssetIAMGroup},
		{"aws:iam/rolePolicyAttachment:RolePolicyAttachment", models.AssetIAMBinding},
		{"aws:iam/instanceProfile:InstanceProfile", models.AssetServiceAccount},
		{"aws:kms/key:Key", models.AssetKMSKey},
		{"aws:secretsmanager/secret:Secret", models.AssetSecret},
		{"aws:sqs/queue:Queue", models.AssetQueue},
		{"aws:sns/topic:Topic", models.AssetPubSub},
		{"aws:acm/certificate:Certificate", models.AssetCertificate},
		{"aws:apigateway/restApi:RestApi", models.AssetAPIGateway},
		{"aws:apigatewayv2/api:Api", models.AssetAPIGateway},
		{"aws:ecs/service:Service", models.AssetService},
		{"aws:ecs/taskDefinition:TaskDefinition", models.AssetContainer},
		{"aws:ecs/cluster:Cluster", models.AssetNode},
		{"aws:eks/cluster:Cluster", models.AssetNode},
		{"aws:lb/loadBalancer:LoadBalancer", models.AssetLoadBalancer},
		{"aws:lb/targetGroup:TargetGroup", models.AssetLoadBalancer},
		{"aws:lb/listener:Listener", models.AssetLoadBalancer},
		{"aws:elasticache/cluster:Cluster", models.AssetDatabase},
		{"aws:autoscaling/group:Group", models.AssetInstanceGroup},
		// GCP
		{"gcp:compute/instance:Instance", models.AssetVM},
		{"gcp:compute/network:Network", models.AssetNetwork},
		{"gcp:compute/subnetwork:Subnetwork", models.AssetSubnet},
		{"gcp:compute/firewall:Firewall", models.AssetFirewallRule},
		{"gcp:compute/disk:Disk", models.AssetDisk},
		{"gcp:compute/address:Address", models.AssetIPAddress},
		{"gcp:compute/forwardingRule:ForwardingRule", models.AssetLoadBalancer},
		{"gcp:compute/healthCheck:HealthCheck", models.AssetHealthCheck},
		{"gcp:compute/backendService:BackendService", models.AssetBackendService},
		{"gcp:compute/backendBucket:BackendBucket", models.AssetCDN},
		{"gcp:compute/instanceGroup:InstanceGroup", models.AssetInstanceGroup},
		{"gcp:storage/bucket:Bucket", models.AssetBucket},
		{"gcp:sql/databaseInstance:DatabaseInstance", models.AssetDatabase},
		{"gcp:redis/instance:Instance", models.AssetDatabase},
		{"gcp:bigquery/dataset:Dataset", models.AssetDatabase},
		{"gcp:dns/recordSet:RecordSet", models.AssetDNSRecord},
		{"gcp:cloudfunctions/function:Function", models.AssetFunction},
		{"gcp:cloudrun/service:Service", models.AssetService},
		{"gcp:serviceAccount/account:Account", models.AssetServiceAccount},
		{"gcp:projects/iAMBinding:IAMBinding", models.AssetIAMBinding},
		{"gcp:kms/keyRing:KeyRing", models.AssetKMSKey},
		{"gcp:kms/cryptoKey:CryptoKey", models.AssetKMSKey},
		{"gcp:secretmanager/secret:Secret", models.AssetSecret},
		{"gcp:pubsub/topic:Topic", models.AssetPubSub},
		{"gcp:pubsub/subscription:Subscription", models.AssetQueue},
		{"gcp:container/cluster:Cluster", models.AssetNode},
		// Azure Native
		{"azure-native:compute:VirtualMachine", models.AssetVM},
		{"azure-native:network:VirtualNetwork", models.AssetNetwork},
		{"azure-native:network:Subnet", models.AssetSubnet},
		{"azure-native:network:NetworkSecurityGroup", models.AssetFirewallRule},
		{"azure-native:network:LoadBalancer", models.AssetLoadBalancer},
		{"azure-native:storage:StorageAccount", models.AssetBucket},
		{"azure-native:sql:Server", models.AssetDatabase},
		{"azure-native:keyvault:Vault", models.AssetKMSKey},
		{"azure-native:containerservice:ManagedCluster", models.AssetNode},
		{"azure-native:authorization:RoleAssignment", models.AssetIAMBinding},
		// Azure Classic
		{"azure:compute/virtualMachine:VirtualMachine", models.AssetVM},
		{"azure:network/virtualNetwork:VirtualNetwork", models.AssetNetwork},
		{"azure:network/subnet:Subnet", models.AssetSubnet},
		{"azure:lb/loadBalancer:LoadBalancer", models.AssetLoadBalancer},
		{"azure:storage/account:Account", models.AssetBucket},
		{"azure:keyvault/keyVault:KeyVault", models.AssetKMSKey},
		{"azure:containerservice/kubernetesCluster:KubernetesCluster", models.AssetNode},
		// Kubernetes
		{"kubernetes:core/v1:Namespace", models.AssetNamespace},
		{"kubernetes:core/v1:Service", models.AssetService},
		{"kubernetes:core/v1:Secret", models.AssetSecret},
		{"kubernetes:apps/v1:Deployment", models.AssetPod},
		{"kubernetes:networking.k8s.io/v1:Ingress", models.AssetIngress},
		// TLS
		{"tls:index/selfSignedCert:SelfSignedCert", models.AssetCertificate},
		{"tls:index/locallySignedCert:LocallySignedCert", models.AssetCertificate},
		// Unmapped
		{"custom:foo/bar:Baz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapPulumiResourceType(tt.input)
			if got != tt.want {
				t.Errorf("mapPulumiResourceType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParsePulumi_InvalidJSON(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(tmp, []byte(`{invalid json`), 0o644)

	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), tmp)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	// Invalid JSON produces warnings (ref map + parse both fail)
	if len(result.Warnings) == 0 {
		t.Error("expected warnings for invalid JSON, got none")
	}
	if len(result.Nodes) != 0 {
		t.Errorf("got %d nodes, want 0", len(result.Nodes))
	}
}

func TestParsePulumi_EmptyState(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "empty.json")
	_ = os.WriteFile(tmp, []byte(`{"version": 3, "deployment": {"resources": []}}`), 0o644)

	p := NewPulumiParser()
	result, err := p.Parse(context.Background(), tmp)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(result.Nodes) != 0 {
		t.Errorf("got %d nodes, want 0", len(result.Nodes))
	}
	if len(result.Edges) != 0 {
		t.Errorf("got %d edges, want 0", len(result.Edges))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsImpl(s, substr)
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

package cloudformation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/matijazezelj/aib/pkg/models"
)

func TestParseCFN_Simple(t *testing.T) {
	p := NewCFNParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "simple.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Nodes) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(result.Nodes))
	}

	nodeMap := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeMap[n.ID] = n
	}

	// Check VPC
	vpc, ok := nodeMap["cfn:network:MyVPC"]
	if !ok {
		t.Fatal("missing node cfn:network:MyVPC")
	}
	if vpc.Type != models.AssetNetwork {
		t.Errorf("VPC type = %q, want %q", vpc.Type, models.AssetNetwork)
	}
	if vpc.Source != "cloudformation" {
		t.Errorf("VPC source = %q, want %q", vpc.Source, "cloudformation")
	}
	if vpc.Provider != "aws" {
		t.Errorf("VPC provider = %q, want %q", vpc.Provider, "aws")
	}

	// Check EC2 instance
	ec2, ok := nodeMap["cfn:vm:MyInstance"]
	if !ok {
		t.Fatal("missing node cfn:vm:MyInstance")
	}
	if ec2.Type != models.AssetVM {
		t.Errorf("EC2 type = %q, want %q", ec2.Type, models.AssetVM)
	}

	// Check subnet
	if _, ok := nodeMap["cfn:subnet:MySubnet"]; !ok {
		t.Fatal("missing node cfn:subnet:MySubnet")
	}

	// Check SG
	if _, ok := nodeMap["cfn:firewall_rule:MySecurityGroup"]; !ok {
		t.Fatal("missing node cfn:firewall_rule:MySecurityGroup")
	}

	// Check bucket
	if _, ok := nodeMap["cfn:bucket:MyBucket"]; !ok {
		t.Fatal("missing node cfn:bucket:MyBucket")
	}
}

func TestParseCFN_Edges(t *testing.T) {
	p := NewCFNParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "simple.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build edge map keyed by type:from->to
	edgeMap := make(map[string]models.Edge)
	for _, e := range result.Edges {
		key := string(e.Type) + ":" + e.FromID + "->" + e.ToID
		edgeMap[key] = e
	}

	// MySubnet -> MyVPC (Ref)
	if _, ok := edgeMap["depends_on:cfn:subnet:MySubnet->cfn:network:MyVPC"]; !ok {
		t.Error("missing edge: MySubnet -> MyVPC (Ref)")
	}

	// MySecurityGroup -> MyVPC (Ref)
	if _, ok := edgeMap["depends_on:cfn:firewall_rule:MySecurityGroup->cfn:network:MyVPC"]; !ok {
		t.Error("missing edge: MySecurityGroup -> MyVPC (Ref)")
	}

	// MyInstance -> MyBucket (DependsOn)
	if e, ok := edgeMap["depends_on:cfn:vm:MyInstance->cfn:bucket:MyBucket"]; !ok {
		t.Error("missing edge: MyInstance -> MyBucket (DependsOn)")
	} else {
		if e.Metadata["via"] != "DependsOn" {
			t.Errorf("DependsOn edge via = %q, want \"DependsOn\"", e.Metadata["via"])
		}
		if e.Metadata["raw_value"] != "MyBucket" {
			t.Errorf("DependsOn edge raw_value = %q, want \"MyBucket\"", e.Metadata["raw_value"])
		}
	}

	// MySubnet -> MyVPC (Ref) should have via=Ref
	if e, ok := edgeMap["depends_on:cfn:subnet:MySubnet->cfn:network:MyVPC"]; !ok {
		t.Error("missing edge: MySubnet -> MyVPC (Ref)")
	} else if e.Metadata["via"] != "Ref" {
		t.Errorf("Ref edge via = %q, want \"Ref\"", e.Metadata["via"])
	}

	// MyInstance -> MySecurityGroup (Ref)
	if _, ok := edgeMap["depends_on:cfn:vm:MyInstance->cfn:firewall_rule:MySecurityGroup"]; !ok {
		t.Error("missing edge: MyInstance -> MySecurityGroup (Ref)")
	}
}

func TestParseCFN_FullStack(t *testing.T) {
	p := NewCFNParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "full_stack.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Nodes) < 15 {
		t.Fatalf("expected at least 15 nodes, got %d", len(result.Nodes))
	}

	nodeMap := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeMap[n.ID] = n
	}

	// Check key resources exist
	checks := []struct {
		id       string
		assetType models.AssetType
	}{
		{"cfn:network:VPC", models.AssetNetwork},
		{"cfn:subnet:PublicSubnet", models.AssetSubnet},
		{"cfn:subnet:PrivateSubnet", models.AssetSubnet},
		{"cfn:firewall_rule:WebSG", models.AssetFirewallRule},
		{"cfn:firewall_rule:AppSG", models.AssetFirewallRule},
		{"cfn:firewall_rule:DBSG", models.AssetFirewallRule},
		{"cfn:load_balancer:ALB", models.AssetLoadBalancer},
		{"cfn:service_account:AppRole", models.AssetServiceAccount},
		{"cfn:vm:AppServer", models.AssetVM},
		{"cfn:database:Database", models.AssetDatabase},
		{"cfn:secret:DBSecret", models.AssetSecret},
		{"cfn:kms_key:EncryptionKey", models.AssetKMSKey},
		{"cfn:dns_record:DNSRecord", models.AssetDNSRecord},
	}

	for _, c := range checks {
		n, ok := nodeMap[c.id]
		if !ok {
			t.Errorf("missing node %s", c.id)
			continue
		}
		if n.Type != c.assetType {
			t.Errorf("node %s type = %q, want %q", c.id, n.Type, c.assetType)
		}
	}

	// Check cross-resource edges
	edgeMap := make(map[string]bool)
	for _, e := range result.Edges {
		key := string(e.Type) + ":" + e.FromID + "->" + e.ToID
		edgeMap[key] = true
	}

	// DNSRecord -> ALB (Fn::GetAtt)
	if !edgeMap["depends_on:cfn:dns_record:DNSRecord->cfn:load_balancer:ALB"] {
		t.Error("missing edge: DNSRecord -> ALB (GetAtt)")
	}

	// Database -> DBSecret (Ref)
	if !edgeMap["depends_on:cfn:database:Database->cfn:secret:DBSecret"] {
		t.Error("missing edge: Database -> DBSecret (Ref)")
	}

	// AppServer -> AppInstanceProfile (DependsOn)
	if !edgeMap["depends_on:cfn:vm:AppServer->cfn:service_account:AppInstanceProfile"] {
		t.Error("missing edge: AppServer -> AppInstanceProfile (DependsOn)")
	}
}

func TestParseCFN_JSON(t *testing.T) {
	p := NewCFNParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "ec2_deps.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Nodes) != 7 {
		t.Fatalf("expected 7 nodes, got %d", len(result.Nodes))
	}

	nodeMap := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeMap[n.ID] = n
	}

	// Check all nodes exist
	expectedNodes := []string{
		"cfn:network:AppVPC",
		"cfn:subnet:AppSubnet",
		"cfn:firewall_rule:AppSG",
		"cfn:queue:AppQueue",
		"cfn:pubsub:AppTopic",
		"cfn:vm:WebServer",
		"cfn:function:LambdaFunction",
	}
	for _, id := range expectedNodes {
		if _, ok := nodeMap[id]; !ok {
			t.Errorf("missing node %s", id)
		}
	}

	// Check edges
	edgeMap := make(map[string]bool)
	for _, e := range result.Edges {
		key := string(e.Type) + ":" + e.FromID + "->" + e.ToID
		edgeMap[key] = true
	}

	// WebServer DependsOn: AppSubnet, AppSG, AppQueue
	if !edgeMap["depends_on:cfn:vm:WebServer->cfn:subnet:AppSubnet"] {
		t.Error("missing DependsOn edge: WebServer -> AppSubnet")
	}
	if !edgeMap["depends_on:cfn:vm:WebServer->cfn:firewall_rule:AppSG"] {
		t.Error("missing DependsOn edge: WebServer -> AppSG")
	}
	if !edgeMap["depends_on:cfn:vm:WebServer->cfn:queue:AppQueue"] {
		t.Error("missing DependsOn edge: WebServer -> AppQueue")
	}

	// AppSubnet DependsOn: AppVPC (single string)
	if !edgeMap["depends_on:cfn:subnet:AppSubnet->cfn:network:AppVPC"] {
		t.Error("missing DependsOn edge: AppSubnet -> AppVPC")
	}

	// Lambda -> AppQueue (Fn::GetAtt)
	if !edgeMap["depends_on:cfn:function:LambdaFunction->cfn:queue:AppQueue"] {
		t.Error("missing Fn::GetAtt edge: Lambda -> AppQueue")
	}

	// Lambda -> AppTopic (Ref)
	if !edgeMap["depends_on:cfn:function:LambdaFunction->cfn:pubsub:AppTopic"] {
		t.Error("missing Ref edge: Lambda -> AppTopic")
	}
}

func TestParseCFN_Metadata(t *testing.T) {
	p := NewCFNParser()
	result, err := p.Parse(context.Background(), filepath.Join("testdata", "ec2_deps.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeMap[n.ID] = n
	}

	// Check InstanceType
	ws := nodeMap["cfn:vm:WebServer"]
	if ws.Metadata["InstanceType"] != "t3.large" {
		t.Errorf("WebServer InstanceType = %q, want %q", ws.Metadata["InstanceType"], "t3.large")
	}
	if ws.Metadata["cfn_type"] != "AWS::EC2::Instance" {
		t.Errorf("WebServer cfn_type = %q, want %q", ws.Metadata["cfn_type"], "AWS::EC2::Instance")
	}

	// Check tags
	if ws.Metadata["tag:Name"] != "web-server" {
		t.Errorf("WebServer tag:Name = %q, want %q", ws.Metadata["tag:Name"], "web-server")
	}
	if ws.Metadata["tag:Team"] != "platform" {
		t.Errorf("WebServer tag:Team = %q, want %q", ws.Metadata["tag:Team"], "platform")
	}

	// Check Lambda runtime
	fn := nodeMap["cfn:function:LambdaFunction"]
	if fn.Metadata["Runtime"] != "python3.12" {
		t.Errorf("Lambda Runtime = %q, want %q", fn.Metadata["Runtime"], "python3.12")
	}
	if fn.Metadata["FunctionName"] != "process-events" {
		t.Errorf("Lambda FunctionName = %q, want %q", fn.Metadata["FunctionName"], "process-events")
	}
}

func TestParseCFN_UnmappedType(t *testing.T) {
	// Create a temp template with an unmapped resource type
	dir := t.TempDir()
	tmpl := `{
		"AWSTemplateFormatVersion": "2010-09-09",
		"Resources": {
			"MyBucket": {
				"Type": "AWS::S3::Bucket",
				"Properties": {"BucketName": "test"}
			},
			"MyCustomThing": {
				"Type": "Custom::MyResource",
				"Properties": {}
			}
		}
	}`
	path := filepath.Join(dir, "custom.json")
	if err := os.WriteFile(path, []byte(tmpl), 0644); err != nil {
		t.Fatal(err)
	}

	p := NewCFNParser()
	result, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the bucket should be parsed
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result.Nodes))
	}
	if result.Nodes[0].ID != "cfn:bucket:MyBucket" {
		t.Errorf("node ID = %q, want %q", result.Nodes[0].ID, "cfn:bucket:MyBucket")
	}

	// Should have a warning about the unmapped type
	found := false
	for _, w := range result.Warnings {
		if w == "unmapped CFN resource type: Custom::MyResource (MyCustomThing)" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about unmapped type, got: %v", result.Warnings)
	}
}

func TestMapCFNResourceType(t *testing.T) {
	tests := []struct {
		cfnType string
		want    models.AssetType
	}{
		{"AWS::EC2::Instance", models.AssetVM},
		{"AWS::Lambda::Function", models.AssetFunction},
		{"AWS::RDS::DBInstance", models.AssetDatabase},
		{"AWS::S3::Bucket", models.AssetBucket},
		{"AWS::EC2::VPC", models.AssetNetwork},
		{"AWS::EC2::Subnet", models.AssetSubnet},
		{"AWS::EC2::SecurityGroup", models.AssetFirewallRule},
		{"AWS::IAM::Role", models.AssetServiceAccount},
		{"AWS::IAM::Policy", models.AssetIAMPolicy},
		{"AWS::KMS::Key", models.AssetKMSKey},
		{"AWS::SecretsManager::Secret", models.AssetSecret},
		{"AWS::SQS::Queue", models.AssetQueue},
		{"AWS::SNS::Topic", models.AssetPubSub},
		{"AWS::Route53::RecordSet", models.AssetDNSRecord},
		{"AWS::CloudFront::Distribution", models.AssetCDN},
		{"AWS::DynamoDB::Table", models.AssetNoSQLDB},
		{"AWS::ECS::Service", models.AssetService},
		{"AWS::ElasticLoadBalancingV2::LoadBalancer", models.AssetLoadBalancer},
		{"AWS::CertificateManager::Certificate", models.AssetCertificate},
		{"Custom::SomeThing", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := mapCFNResourceType(tt.cfnType)
		if got != tt.want {
			t.Errorf("mapCFNResourceType(%q) = %q, want %q", tt.cfnType, got, tt.want)
		}
	}
}

func TestSupported(t *testing.T) {
	p := NewCFNParser()

	// Real templates
	if !p.Supported(filepath.Join("testdata", "simple.yaml")) {
		t.Error("simple.yaml should be supported")
	}
	if !p.Supported(filepath.Join("testdata", "ec2_deps.json")) {
		t.Error("ec2_deps.json should be supported")
	}

	// Non-existent file
	if p.Supported("/nonexistent/file.yaml") {
		t.Error("non-existent file should not be supported")
	}

	// Wrong extension
	dir := t.TempDir()
	txtFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(txtFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if p.Supported(txtFile) {
		t.Error(".txt file should not be supported")
	}

	// YAML without CFN markers
	nonCfn := filepath.Join(dir, "docker-compose.yaml")
	if err := os.WriteFile(nonCfn, []byte("services:\n  web:\n    image: nginx"), 0644); err != nil {
		t.Fatal(err)
	}
	if p.Supported(nonCfn) {
		t.Error("non-CFN YAML should not be supported")
	}
}

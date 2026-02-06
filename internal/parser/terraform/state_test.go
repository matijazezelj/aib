package terraform

import (
	"context"
	"os"
	"testing"

	"github.com/matijazezelj/aib/pkg/models"
)

func TestParseStateFile_Sample(t *testing.T) {
	p := NewStateParser()
	result, err := p.Parse(context.Background(), "testdata/sample.tfstate")
	if err != nil {
		// Use absolute path from testdata directory
		wd, _ := os.Getwd()
		t.Fatalf("Parse failed (wd=%s): %v", wd, err)
	}

	if len(result.Nodes) != 6 {
		t.Errorf("nodes = %d, want 6", len(result.Nodes))
	}

	// Check specific node IDs exist
	nodeIDs := make(map[string]bool)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = true
	}

	want := []string{
		"tf:vm:web-prod-1",
		"tf:network:prod-vpc",
		"tf:subnet:prod-subnet",
		"tf:database:cloudsql-prod",
		"tf:dns_record:web.example.com",
		"tf:bucket:myproj-assets",
	}
	for _, id := range want {
		if !nodeIDs[id] {
			t.Errorf("missing node %s", id)
		}
	}
}

func TestParseStateFile_Edges(t *testing.T) {
	result, err := parseStateFile("testdata/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	// Should have dependency edges + attribute edges (network/subnetwork connects_to)
	if len(result.Edges) == 0 {
		t.Fatal("expected edges, got 0")
	}

	// Check that dependency edges exist (key includes type since multiple
	// edge types can exist between the same node pair).
	edgeSet := make(map[string]bool)
	for _, e := range result.Edges {
		edgeSet[string(e.Type)+":"+e.FromID+"->"+e.ToID] = true
	}

	// web-prod-1 depends_on prod-vpc (via tfstate dependency)
	if !edgeSet[string(models.EdgeDependsOn)+":tf:vm:web-prod-1->tf:network:prod-vpc"] {
		t.Error("missing depends_on edge from vm to network")
	}

	// cloudsql-prod depends_on prod-vpc
	if !edgeSet[string(models.EdgeDependsOn)+":tf:database:cloudsql-prod->tf:network:prod-vpc"] {
		t.Error("missing depends_on edge from database to network")
	}
}

func TestParseStateFile_Metadata(t *testing.T) {
	result, err := parseStateFile("testdata/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range result.Nodes {
		if n.ID == "tf:vm:web-prod-1" {
			if n.Metadata["zone"] != "us-central1-a" {
				t.Errorf("zone = %q, want us-central1-a", n.Metadata["zone"])
			}
			if n.Metadata["machine_type"] != "e2-medium" {
				t.Errorf("machine_type = %q, want e2-medium", n.Metadata["machine_type"])
			}
			if n.Metadata["tf_type"] != "google_compute_instance" {
				t.Errorf("tf_type = %q, want google_compute_instance", n.Metadata["tf_type"])
			}
			if n.Provider != "google" {
				t.Errorf("provider = %q, want google", n.Provider)
			}
			return
		}
	}
	t.Error("tf:vm:web-prod-1 not found")
}

func TestParseStateFile_SourceField(t *testing.T) {
	result, err := parseStateFile("testdata/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range result.Nodes {
		if n.Source != "terraform" {
			t.Errorf("node %s source = %q, want terraform", n.ID, n.Source)
		}
	}
}

func TestMapResourceType(t *testing.T) {
	tests := []struct {
		tfType string
		want   models.AssetType
	}{
		{"google_compute_instance", models.AssetVM},
		{"aws_instance", models.AssetVM},
		{"azurerm_virtual_machine", models.AssetVM},
		{"google_sql_database_instance", models.AssetDatabase},
		{"aws_s3_bucket", models.AssetBucket},
		{"google_compute_network", models.AssetNetwork},
		{"aws_vpc", models.AssetNetwork},
		{"cloudflare_record", models.AssetDNSRecord},
		{"tls_self_signed_cert", models.AssetCertificate},
		{"kubernetes_deployment", models.AssetPod},
		// IAM / Identity
		{"google_project_iam_binding", models.AssetIAMBinding},
		{"google_project_iam_policy", models.AssetIAMPolicy},
		{"aws_iam_role", models.AssetServiceAccount},
		{"aws_iam_policy", models.AssetIAMPolicy},
		{"aws_iam_role_policy_attachment", models.AssetIAMBinding},
		{"aws_iam_group", models.AssetIAMGroup},
		{"azurerm_role_assignment", models.AssetIAMBinding},
		{"google_service_account", models.AssetServiceAccount},
		// KMS
		{"google_kms_key_ring", models.AssetKMSKey},
		{"google_kms_crypto_key", models.AssetKMSKey},
		{"aws_kms_key", models.AssetKMSKey},
		// CDN
		{"aws_cloudfront_distribution", models.AssetCDN},
		{"google_compute_backend_bucket", models.AssetCDN},
		// Disk
		{"google_compute_disk", models.AssetDisk},
		{"aws_ebs_volume", models.AssetDisk},
		// Instance Groups
		{"google_compute_instance_group_manager", models.AssetInstanceGroup},
		{"aws_autoscaling_group", models.AssetInstanceGroup},
		// Health / Backend
		{"google_compute_health_check", models.AssetHealthCheck},
		{"google_compute_backend_service", models.AssetBackendService},
		// S3 sub-resources
		{"aws_s3_bucket_acl", models.AssetIAMPolicy},
		{"aws_s3_bucket_versioning", models.AssetBucket},
		{"aws_s3_bucket_policy", models.AssetIAMPolicy},
		// Monitoring
		{"pingdom_check", models.AssetMonitor},
		// Serverless
		{"aws_lambda_function", models.AssetFunction},
		{"aws_api_gateway_rest_api", models.AssetAPIGateway},
		{"aws_apigatewayv2_api", models.AssetAPIGateway},
		{"aws_dynamodb_table", models.AssetNoSQLDB},
		{"aws_secretsmanager_secret", models.AssetSecret},
		{"google_cloudfunctions_function", models.AssetFunction},
		{"google_cloudfunctions2_function", models.AssetFunction},
		{"google_cloud_run_service", models.AssetService},
		{"google_cloud_run_v2_service", models.AssetService},
		{"google_bigquery_dataset", models.AssetDatabase},
		{"google_bigquery_table", models.AssetDatabase},
		{"azurerm_function_app", models.AssetFunction},
		{"azurerm_linux_function_app", models.AssetFunction},
		{"azurerm_windows_function_app", models.AssetFunction},
		{"azurerm_cosmosdb_account", models.AssetNoSQLDB},
		{"azurerm_api_management", models.AssetAPIGateway},
		{"unknown_resource_type", ""},
	}

	for _, tt := range tests {
		got := mapResourceType(tt.tfType)
		if got != tt.want {
			t.Errorf("mapResourceType(%q) = %q, want %q", tt.tfType, got, tt.want)
		}
	}
}

func TestExtractProvider(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`provider["registry.terraform.io/hashicorp/google"]`, "google"},
		{`provider["registry.terraform.io/hashicorp/aws"]`, "aws"},
		{`provider["registry.terraform.io/cloudflare/cloudflare"]`, "cloudflare"},
		{"google", "google"},
	}

	for _, tt := range tests {
		got := extractProvider(tt.input)
		if got != tt.want {
			t.Errorf("extractProvider(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractMetadata(t *testing.T) {
	attrs := map[string]any{
		"region":       "us-east-1",
		"zone":         "us-east-1a",
		"machine_type": "n1-standard-1",
		"name":         "test-vm",
		"tags":         map[string]any{"env": "prod"},
		"labels":       map[string]any{"team": "infra"},
	}

	meta := extractMetadata("google_compute_instance", attrs)

	if meta["region"] != "us-east-1" {
		t.Errorf("region = %q", meta["region"])
	}
	if meta["tag:env"] != "prod" {
		t.Errorf("tag:env = %q", meta["tag:env"])
	}
	if meta["label:team"] != "infra" {
		t.Errorf("label:team = %q", meta["label:team"])
	}
	if meta["tf_type"] != "google_compute_instance" {
		t.Errorf("tf_type = %q", meta["tf_type"])
	}
}

func TestParseStateBytes_InvalidJSON(t *testing.T) {
	_, err := parseStateBytes([]byte("{invalid"), "test.tfstate")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseDirectory_Recursive(t *testing.T) {
	p := NewStateParser()

	if !p.Supported("testdata/nested") {
		t.Fatal("Supported() should return true for directory with nested .tfstate")
	}

	result, err := p.Parse(context.Background(), "testdata/nested")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Nodes) == 0 {
		t.Fatal("expected nodes from nested directory, got 0")
	}

	found := false
	for _, n := range result.Nodes {
		if n.ID == "tf:vm:deep-nested-vm" {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing tf:vm:deep-nested-vm from nested state file")
	}
}

func TestParseCrossState_Edges(t *testing.T) {
	p := NewStateParser()
	result, err := p.Parse(context.Background(), "testdata/cross-state")
	if err != nil {
		t.Fatal(err)
	}

	// Should have nodes from both state files
	nodeIDs := make(map[string]bool)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = true
	}

	if !nodeIDs["tf:network:shared-vpc"] {
		t.Error("missing tf:network:shared-vpc from networking state")
	}
	if !nodeIDs["tf:vm:app-server-1"] {
		t.Error("missing tf:vm:app-server-1 from compute state")
	}

	// Cross-state edges: VM depends_on network (defined in different state file)
	edgeSet := make(map[string]bool)
	for _, e := range result.Edges {
		edgeSet[string(e.Type)+":"+e.FromID+"->"+e.ToID] = true
	}

	if !edgeSet[string(models.EdgeDependsOn)+":tf:vm:app-server-1->tf:network:shared-vpc"] {
		t.Error("missing cross-state depends_on edge from vm to network")
	}
	if !edgeSet[string(models.EdgeDependsOn)+":tf:vm:app-server-1->tf:subnet:app-subnet"] {
		t.Error("missing cross-state depends_on edge from vm to subnet")
	}

	// Attribute edges should also resolve cross-state
	if !edgeSet[string(models.EdgeConnectsTo)+":tf:vm:app-server-1->tf:network:shared-vpc"] {
		t.Error("missing cross-state connects_to edge from vm to network")
	}
	if !edgeSet[string(models.EdgeConnectsTo)+":tf:vm:app-server-1->tf:subnet:app-subnet"] {
		t.Error("missing cross-state connects_to edge from vm to subnet")
	}
}

func TestParseMulti_SeparateFiles(t *testing.T) {
	p := NewStateParser()
	result, err := p.ParseMulti(context.Background(), []string{
		"testdata/cross-state/networking.tfstate",
		"testdata/cross-state/compute.tfstate",
	})
	if err != nil {
		t.Fatal(err)
	}

	nodeIDs := make(map[string]bool)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = true
	}

	if !nodeIDs["tf:network:shared-vpc"] {
		t.Error("missing tf:network:shared-vpc")
	}
	if !nodeIDs["tf:vm:app-server-1"] {
		t.Error("missing tf:vm:app-server-1")
	}

	// Cross-state edges should resolve even when paths are separate files
	edgeSet := make(map[string]bool)
	for _, e := range result.Edges {
		edgeSet[string(e.Type)+":"+e.FromID+"->"+e.ToID] = true
	}

	if !edgeSet[string(models.EdgeDependsOn)+":tf:vm:app-server-1->tf:network:shared-vpc"] {
		t.Error("missing cross-state depends_on edge from vm to network via ParseMulti")
	}
	if !edgeSet[string(models.EdgeConnectsTo)+":tf:vm:app-server-1->tf:network:shared-vpc"] {
		t.Error("missing cross-state connects_to edge from vm to network via ParseMulti")
	}
}

func TestParseStateBytes_DataResourceSkipped(t *testing.T) {
	state := `{
		"version": 4,
		"resources": [{
			"mode": "data",
			"type": "google_compute_instance",
			"name": "lookup",
			"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
			"instances": [{"attributes": {"name": "lookup-vm"}, "dependencies": []}]
		}]
	}`

	result, err := parseStateBytes([]byte(state), "test.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 0 {
		t.Errorf("data resources should be skipped, got %d nodes", len(result.Nodes))
	}
}

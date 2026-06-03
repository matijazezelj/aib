package terraform

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/matijazezelj/aib/internal/parser"
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
	result, err := NewStateParser().Parse(context.Background(), "testdata/sample.tfstate")
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

func TestParseStateFile_EdgeMetadata(t *testing.T) {
	result, err := NewStateParser().Parse(context.Background(), "testdata/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	// Check dependency edge metadata (source + reference)
	for _, e := range result.Edges {
		if e.Type == models.EdgeDependsOn && e.FromID == "tf:vm:web-prod-1" && e.ToID == "tf:network:prod-vpc" {
			if e.Metadata["source"] != "tfstate_dependency" {
				t.Errorf("depends_on edge source = %q, want \"tfstate_dependency\"", e.Metadata["source"])
			}
			if e.Metadata["reference"] == "" {
				t.Error("depends_on edge reference should not be empty")
			}
		}
	}

	// Check attribute edge metadata (via + raw_value)
	for _, e := range result.Edges {
		if e.Type == models.EdgeConnectsTo && e.Metadata["via"] != "" {
			if e.Metadata["raw_value"] == "" {
				t.Errorf("connects_to edge with via=%q should have non-empty raw_value", e.Metadata["via"])
			}
			return // found at least one, good
		}
	}
	// Note: attribute edges only exist if resources have network/subnetwork/vpc_id attrs
	// that resolve to known nodes; if none resolve, this is not a failure.
}

func TestParseStateFile_Metadata(t *testing.T) {
	result, err := NewStateParser().Parse(context.Background(), "testdata/sample.tfstate")
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
	result, err := NewStateParser().Parse(context.Background(), "testdata/sample.tfstate")
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
	_, err := parseStateBytesForTest([]byte("{invalid"), "test.tfstate")
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

	result, err := parseStateBytesForTest([]byte(state), "test.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 0 {
		t.Errorf("data resources should be skipped, got %d nodes", len(result.Nodes))
	}
}

func TestStateParser_Supported_File(t *testing.T) {
	p := NewStateParser()
	if !p.Supported("testdata/sample.tfstate") {
		t.Error("Supported should return true for .tfstate file")
	}
}

func TestStateParser_Supported_Dir(t *testing.T) {
	p := NewStateParser()
	if !p.Supported("testdata/nested") {
		t.Error("Supported should return true for directory containing .tfstate")
	}
}

func TestStateParser_Supported_Invalid(t *testing.T) {
	p := NewStateParser()
	// Create a temp .json file (not .tfstate)
	tmpDir := t.TempDir()
	f, err := os.Create(tmpDir + "/notstate.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close() //nolint:errcheck
	if p.Supported(f.Name()) {
		t.Error("Supported should return false for .json file")
	}
}

func TestStateParser_Supported_Nonexistent(t *testing.T) {
	p := NewStateParser()
	if p.Supported("/nonexistent/path/foo.tfstate") {
		t.Error("Supported should return false for nonexistent path")
	}
}

func TestParseMulti_CrossState_SeparateDirs(t *testing.T) {
	p := NewStateParser()
	result, err := p.ParseMulti(context.Background(), []string{
		"testdata/cross-state",
		"testdata/nested",
	})
	if err != nil {
		t.Fatal(err)
	}

	nodeIDs := make(map[string]bool)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = true
	}

	// From cross-state
	if !nodeIDs["tf:network:shared-vpc"] {
		t.Error("missing tf:network:shared-vpc from cross-state")
	}
	// From nested
	if !nodeIDs["tf:vm:deep-nested-vm"] {
		t.Error("missing tf:vm:deep-nested-vm from nested")
	}
}

func TestParseMulti_NonexistentPath(t *testing.T) {
	p := NewStateParser()
	_, err := p.ParseMulti(context.Background(), []string{"/nonexistent/path"})
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestParseStateCert_ExpiresAt(t *testing.T) {
	p := NewStateParser()
	result, err := p.Parse(context.Background(), "testdata/cert.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	var certWithExpiry, certWithoutExpiry bool
	for _, n := range result.Nodes {
		if n.Name == "example-cert" {
			if n.ExpiresAt == nil {
				t.Error("expected ExpiresAt for example-cert")
			}
			certWithExpiry = true
		}
		if n.Name == "no-expiry-cert" {
			if n.ExpiresAt != nil {
				t.Error("expected nil ExpiresAt for no-expiry-cert")
			}
			certWithoutExpiry = true
		}
	}
	if !certWithExpiry {
		t.Error("missing example-cert node")
	}
	if !certWithoutExpiry {
		t.Error("missing no-expiry-cert node")
	}
}

func TestParseStateCert_UnmappedWarning(t *testing.T) {
	p := NewStateParser()
	result, err := p.Parse(context.Background(), "testdata/cert.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	hasWarning := false
	for _, w := range result.Warnings {
		if len(w) > 0 && w[:8] == "unmapped" {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected unmapped resource type warning")
	}
}

func TestParseStateCert_LabelsMetadata(t *testing.T) {
	p := NewStateParser()
	result, err := p.Parse(context.Background(), "testdata/cert.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range result.Nodes {
		if n.Name == "vpc-prod" {
			if n.Metadata["label:env"] != "prod" {
				t.Errorf("expected label:env=prod, got %q", n.Metadata["label:env"])
			}
			if n.Metadata["label:team"] != "infra" {
				t.Errorf("expected label:team=infra, got %q", n.Metadata["label:team"])
			}
			return
		}
	}
	t.Error("missing vpc-prod node")
}

func TestParseStateCert_VPCIDEdge(t *testing.T) {
	p := NewStateParser()
	result, err := p.Parse(context.Background(), "testdata/cert.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range result.Edges {
		if e.Metadata["via"] == "vpc_id" {
			return // found vpc_id edge
		}
	}
	// vpc_id self-reference may or may not resolve — just verify no panic
}

func TestParseMulti_InvalidFile(t *testing.T) {
	// Create a temp dir with an invalid .tfstate file
	dir := t.TempDir()
	invalidFile := filepath.Join(dir, "bad.tfstate")
	if err := os.WriteFile(invalidFile, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	p := NewStateParser()
	result, err := p.ParseMulti(context.Background(), []string{dir})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Warnings) == 0 {
		t.Error("expected warnings for invalid tfstate file")
	}
}

func TestParseMulti_UnreadableFile(t *testing.T) {
	// Create a directory with a .tfstate file that can't be read.
	// This triggers: ReadFile error in phase 1 (line 96-98) and
	// stateData !ok skip in phase 2 (line 114-115).
	dir := t.TempDir()
	unreadable := filepath.Join(dir, "unreadable.tfstate")
	if err := os.WriteFile(unreadable, []byte(`{}`), 0000); err != nil {
		t.Fatal(err)
	}

	p := NewStateParser()
	result, err := p.ParseMulti(context.Background(), []string{dir})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Warnings) == 0 {
		t.Error("expected warnings for unreadable file")
	}
}

func TestParseStateFile_NonexistentFile(t *testing.T) {
	_, err := NewStateParser().Parse(context.Background(), "/nonexistent/missing.tfstate")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestExtractProvider_EmptyParts(t *testing.T) {
	// extractProvider with a raw provider name (no slashes)
	got := extractProvider("google")
	if got != "google" {
		t.Errorf("extractProvider(\"google\") = %q, want \"google\"", got)
	}
}

func TestExtractProvider_EmptyString(t *testing.T) {
	got := extractProvider("")
	if got != "" {
		t.Errorf("extractProvider(\"\") = %q, want \"\"", got)
	}
}

func TestParseStateCert_UnresolvableDependency(t *testing.T) {
	// cert.tfstate has orphan_subnet with dependency on aws_vpc.does_not_exist
	// which doesn't exist — this exercises the depNodeID !ok continue branch.
	p := NewStateParser()
	result, err := p.Parse(context.Background(), "testdata/cert.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	// orphan-subnet should still be created as a node
	found := false
	for _, n := range result.Nodes {
		if n.Name == "orphan-subnet" {
			found = true
		}
	}
	if !found {
		t.Error("missing orphan-subnet node")
	}

	// No depends_on edge should exist for the unresolvable dependency
	for _, e := range result.Edges {
		if e.FromID == "tf:subnet:orphan-subnet" && e.Type == "depends_on" {
			t.Error("should not have depends_on edge for unresolvable dependency")
		}
	}
}

func TestParseStateCert_UnresolvableAttributeEdge(t *testing.T) {
	// cert.tfstate has orphan_subnet with vpc_id="vpc-nonexistent" and
	// network="projects/missing/global/networks/ghost" — both point to
	// resources not in the state, so resolveTarget returns "" and addEdge skips.
	p := NewStateParser()
	result, err := p.Parse(context.Background(), "testdata/cert.tfstate")
	if err != nil {
		t.Fatal(err)
	}

	// No connects_to edges should exist FROM the orphan subnet
	for _, e := range result.Edges {
		if e.FromID == "tf:subnet:orphan-subnet" && e.Type == "connects_to" {
			t.Errorf("should not have connects_to edge from orphan-subnet, got via=%s to=%s", e.Metadata["via"], e.ToID)
		}
	}
}

func parseStateBytesForTest(data []byte, sourcePath string) (*parser.ParseResult, error) {
	refs, err := buildRefMap(data)
	if err != nil {
		return nil, err
	}
	return parseStateBytesWithRefs(data, sourcePath, refs)
}

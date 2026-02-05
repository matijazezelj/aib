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

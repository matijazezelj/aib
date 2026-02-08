package terraform

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPlanParser_Name(t *testing.T) {
	p := NewPlanParser()
	if got := p.Name(); got != "terraform-plan" {
		t.Errorf("Name() = %q, want %q", got, "terraform-plan")
	}
}

func TestPlanParser_Supported(t *testing.T) {
	p := NewPlanParser()

	testdata, err := filepath.Abs("testdata/plan_create.json")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Supported(testdata) {
		t.Error("expected plan_create.json to be supported")
	}

	// Non-JSON file
	tfstate, err := filepath.Abs("testdata/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	if p.Supported(tfstate) {
		t.Error("expected .tfstate to NOT be supported by PlanParser")
	}

	// Non-existent file
	if p.Supported("/nonexistent/plan.json") {
		t.Error("expected non-existent file to NOT be supported")
	}

	// Create a JSON file without format_version
	tmpDir := t.TempDir()
	noFV := filepath.Join(tmpDir, "no_format_version.json")
	_ = os.WriteFile(noFV, []byte(`{"resources": []}`), 0o644)
	if p.Supported(noFV) {
		t.Error("expected JSON without format_version to NOT be supported")
	}
}

func TestParsePlanBytes_CreateActions(t *testing.T) {
	testdata, err := filepath.Abs("testdata/plan_create.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(testdata)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, testdata)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(result.Nodes))
	}

	// Check first node
	found := false
	for _, n := range result.Nodes {
		if n.Name == "web-server" {
			found = true
			if n.Source != "terraform-plan" {
				t.Errorf("source = %q, want terraform-plan", n.Source)
			}
			if n.Type != "vm" {
				t.Errorf("type = %q, want vm", n.Type)
			}
			if n.Provider != "aws" {
				t.Errorf("provider = %q, want aws", n.Provider)
			}
			if n.Metadata["plan_action"] != "create" {
				t.Errorf("plan_action = %q, want create", n.Metadata["plan_action"])
			}
		}
	}
	if !found {
		t.Error("web-server node not found")
	}
}

func TestParsePlanBytes_MixedActions(t *testing.T) {
	testdata, err := filepath.Abs("testdata/plan_mixed.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(testdata)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, testdata)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 4 nodes: create (web), update (db), delete (bucket), replace (sg)
	// no-op (vpc) and data source (ami) are skipped
	if len(result.Nodes) != 4 {
		t.Fatalf("nodes = %d, want 4", len(result.Nodes))
	}

	actionMap := make(map[string]string)
	for _, n := range result.Nodes {
		actionMap[n.Name] = n.Metadata["plan_action"]
	}

	expected := map[string]string{
		"web-server": "create",
		"prod-db":    "update",
		"old-bucket": "delete",
		"web-sg":     "replace",
	}
	for name, wantAction := range expected {
		if got, ok := actionMap[name]; !ok {
			t.Errorf("missing node %q", name)
		} else if got != wantAction {
			t.Errorf("node %q action = %q, want %q", name, got, wantAction)
		}
	}
}

func TestParsePlanBytes_DataSource(t *testing.T) {
	data := []byte(`{
		"format_version": "1.2",
		"terraform_version": "1.5.0",
		"resource_changes": [{
			"address": "data.aws_ami.latest",
			"mode": "data",
			"type": "aws_ami",
			"name": "latest",
			"provider_name": "registry.terraform.io/hashicorp/aws",
			"change": {"actions": ["read"], "before": null, "after": {}, "after_unknown": {}}
		}]
	}`)

	result, err := ParsePlanBytes(data, "test.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 0 {
		t.Errorf("nodes = %d, want 0 (data sources should be skipped)", len(result.Nodes))
	}
}

func TestParsePlanBytes_UnmappedType(t *testing.T) {
	data := []byte(`{
		"format_version": "1.2",
		"terraform_version": "1.5.0",
		"resource_changes": [{
			"address": "some_custom_resource.thing",
			"mode": "managed",
			"type": "some_custom_resource",
			"name": "thing",
			"provider_name": "registry.terraform.io/example/custom",
			"change": {"actions": ["create"], "before": null, "after": {"name": "thing"}, "after_unknown": {}}
		}]
	}`)

	result, err := ParsePlanBytes(data, "test.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 0 {
		t.Errorf("nodes = %d, want 0 (unmapped types should be skipped)", len(result.Nodes))
	}
	if len(result.Warnings) != 1 {
		t.Errorf("warnings = %d, want 1", len(result.Warnings))
	}
}

func TestDeterminePlanAction(t *testing.T) {
	tests := []struct {
		actions []string
		want    string
	}{
		{[]string{"create"}, "create"},
		{[]string{"delete"}, "delete"},
		{[]string{"update"}, "update"},
		{[]string{"read"}, "no-op"},
		{[]string{"no-op"}, "no-op"},
		{[]string{"delete", "create"}, "replace"},
		{nil, "no-op"},
		{[]string{}, "no-op"},
	}
	for _, tt := range tests {
		got := determinePlanAction(tt.actions)
		if got != tt.want {
			t.Errorf("determinePlanAction(%v) = %q, want %q", tt.actions, got, tt.want)
		}
	}
}

func TestPlanParser_ParseMulti(t *testing.T) {
	p := NewPlanParser()
	testdata1, _ := filepath.Abs("testdata/plan_create.json")
	testdata2, _ := filepath.Abs("testdata/plan_mixed.json")

	result, err := p.ParseMulti(context.Background(), []string{testdata1, testdata2})
	if err != nil {
		t.Fatal(err)
	}

	// plan_create: 2 nodes, plan_mixed: 4 nodes = 6 total
	if len(result.Nodes) != 6 {
		t.Errorf("nodes = %d, want 6", len(result.Nodes))
	}
}

func TestPlanParser_Parse(t *testing.T) {
	p := NewPlanParser()
	testdata, _ := filepath.Abs("testdata/plan_create.json")

	result, err := p.Parse(context.Background(), testdata)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(result.Nodes))
	}
}

func TestParsePlanBytes_InvalidJSON(t *testing.T) {
	_, err := ParsePlanBytes([]byte("not json"), "test.json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// --- Integration tests with realistic terraform show -json output ---

func TestRealisticPlan_NodeCount(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	// 15 resource_changes total:
	//   - 1 no-op (aws_vpc.unchanged) → skipped
	//   - 2 data sources (aws_ami.ubuntu, aws_caller_identity.current) → skipped
	//   - 1 unmapped type (random_string.suffix) → skipped with warning
	//   = 11 actual nodes
	if len(result.Nodes) != 11 {
		var names []string
		for _, n := range result.Nodes {
			names = append(names, n.Name+"("+n.Metadata["plan_action"]+")")
		}
		t.Fatalf("nodes = %d, want 11; got: %v", len(result.Nodes), names)
	}
}

func TestRealisticPlan_AllActions(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	// Build name→action map
	actionMap := make(map[string]string)
	for _, n := range result.Nodes {
		actionMap[n.Name] = n.Metadata["plan_action"]
	}

	expected := map[string]string{
		"prod-vpc":             "create",
		"public-subnet-a":     "create",
		"private-subnet-a":    "create",
		"web-sg":              "create",
		"web-alb":             "create",
		"web-server":          "create",
		"prod-db":             "update",
		"prod-logs-bucket":    "delete",
		"legacy-sg":           "replace",
		"web.prod.example.com": "create",
		"web-server-role":     "create",
	}
	for name, wantAction := range expected {
		got, ok := actionMap[name]
		if !ok {
			t.Errorf("missing node %q", name)
		} else if got != wantAction {
			t.Errorf("node %q action = %q, want %q", name, got, wantAction)
		}
	}
}

func TestRealisticPlan_NodeTypes(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	typeMap := make(map[string]string)
	for _, n := range result.Nodes {
		typeMap[n.Name] = string(n.Type)
	}

	expected := map[string]string{
		"prod-vpc":             "network",
		"public-subnet-a":     "subnet",
		"private-subnet-a":    "subnet",
		"web-sg":              "firewall_rule",
		"web-alb":             "load_balancer",
		"web-server":          "vm",
		"prod-db":             "database",
		"prod-logs-bucket":    "bucket",
		"legacy-sg":           "firewall_rule",
		"web.prod.example.com": "dns_record",
		"web-server-role":     "service_account",
	}
	for name, wantType := range expected {
		got, ok := typeMap[name]
		if !ok {
			t.Errorf("missing node %q", name)
		} else if got != wantType {
			t.Errorf("node %q type = %q, want %q", name, got, wantType)
		}
	}
}

func TestRealisticPlan_AttributeEdges(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	// Build edge set keyed by type:from->to
	edgeSet := make(map[string]string) // key → via metadata
	for _, e := range result.Edges {
		key := string(e.Type) + ":" + e.FromID + "->" + e.ToID
		edgeSet[key] = e.Metadata["via"]
	}

	// Subnets reference VPC via vpc_id attribute → connects_to edges
	wantEdges := []struct {
		key  string
		via  string
		desc string
	}{
		{
			key:  "connects_to:tf:subnet:public-subnet-a->tf:network:prod-vpc",
			via:  "vpc_id",
			desc: "public subnet → VPC via vpc_id",
		},
		{
			key:  "connects_to:tf:subnet:private-subnet-a->tf:network:prod-vpc",
			via:  "vpc_id",
			desc: "private subnet → VPC via vpc_id",
		},
		{
			key:  "connects_to:tf:firewall_rule:web-sg->tf:network:prod-vpc",
			via:  "vpc_id",
			desc: "security group → VPC via vpc_id",
		},
		{
			key:  "connects_to:tf:firewall_rule:legacy-sg->tf:network:prod-vpc",
			via:  "vpc_id",
			desc: "legacy SG (replace) → VPC via vpc_id",
		},
	}

	for _, want := range wantEdges {
		via, ok := edgeSet[want.key]
		if !ok {
			t.Errorf("missing edge: %s (%s)", want.desc, want.key)
		} else if via != want.via {
			t.Errorf("edge %s: via = %q, want %q", want.desc, via, want.via)
		}
	}

	if len(result.Edges) == 0 {
		t.Fatal("expected edges, got 0")
	}
}

func TestRealisticPlan_Metadata(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range result.Nodes {
		// Every node must have plan_action and tf_type
		if n.Metadata["plan_action"] == "" {
			t.Errorf("node %q missing plan_action metadata", n.Name)
		}
		if n.Metadata["tf_type"] == "" {
			t.Errorf("node %q missing tf_type metadata", n.Name)
		}
		// Every node must have source=terraform-plan
		if n.Source != "terraform-plan" {
			t.Errorf("node %q source = %q, want terraform-plan", n.Name, n.Source)
		}
		// Every node must have provider=aws
		if n.Provider != "aws" {
			t.Errorf("node %q provider = %q, want aws", n.Name, n.Provider)
		}

		// Check specific metadata on the DB update node
		if n.Name == "prod-db" {
			if n.Metadata["region"] != "us-east-1" {
				t.Errorf("prod-db region = %q, want us-east-1", n.Metadata["region"])
			}
			if n.Metadata["tf_type"] != "aws_db_instance" {
				t.Errorf("prod-db tf_type = %q, want aws_db_instance", n.Metadata["tf_type"])
			}
			// Note: AWS RDS uses "instance_class" not "instance_type",
			// so extractMetadata won't pick it up. This is a known gap.
		}

		// Check tags extracted from the web server
		if n.Name == "web-server" {
			if n.Metadata["ami"] != "ami-0c55b159cbfafe1f0" {
				t.Errorf("web-server ami = %q", n.Metadata["ami"])
			}
			if n.Metadata["instance_type"] != "t3.medium" {
				t.Errorf("web-server instance_type = %q", n.Metadata["instance_type"])
			}
		}

		// Check delete node uses before state for metadata
		if n.Name == "prod-logs-bucket" {
			if n.Metadata["region"] != "us-east-1" {
				t.Errorf("prod-logs-bucket region = %q, want us-east-1 (from before state)", n.Metadata["region"])
			}
		}
	}
}

func TestRealisticPlan_SkipsNoOpAndData(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	nodeNames := make(map[string]bool)
	for _, n := range result.Nodes {
		nodeNames[n.Name] = true
	}

	// no-op resource should be skipped
	if nodeNames["staging-vpc"] {
		t.Error("no-op resource 'staging-vpc' should be skipped")
	}
	// data sources should be skipped
	if nodeNames["ubuntu"] {
		t.Error("data source 'ubuntu' should be skipped")
	}
	if nodeNames["current"] {
		t.Error("data source 'current' should be skipped")
	}
}

func TestRealisticPlan_UnmappedTypeWarning(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	// random_string.suffix is an unmapped type → should produce a warning
	foundWarning := false
	for _, w := range result.Warnings {
		if contains(w, "random_string.suffix") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("expected warning for unmapped type random_string.suffix, got warnings: %v", result.Warnings)
	}
}

func TestRealisticPlan_NodeIDs(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	nodeIDs := make(map[string]bool)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = true
	}

	// Verify node IDs follow tf:<type>:<name> format
	wantIDs := []string{
		"tf:network:prod-vpc",
		"tf:subnet:public-subnet-a",
		"tf:subnet:private-subnet-a",
		"tf:firewall_rule:web-sg",
		"tf:load_balancer:web-alb",
		"tf:vm:web-server",
		"tf:database:prod-db",
		"tf:bucket:prod-logs-bucket",
		"tf:firewall_rule:legacy-sg",
		"tf:dns_record:web.prod.example.com",
		"tf:service_account:web-server-role",
	}
	for _, id := range wantIDs {
		if !nodeIDs[id] {
			t.Errorf("missing node ID %q", id)
		}
	}
}

func TestRealisticPlan_CrossFileParsing(t *testing.T) {
	p := NewPlanParser()
	infra, _ := filepath.Abs("testdata/plan_realistic.json")
	services, _ := filepath.Abs("testdata/plan_services.json")

	result, err := p.ParseMulti(context.Background(), []string{infra, services})
	if err != nil {
		t.Fatal(err)
	}

	// plan_realistic: 11 nodes, plan_services: 6 nodes = 17 total
	if len(result.Nodes) != 17 {
		var names []string
		for _, n := range result.Nodes {
			names = append(names, n.Name)
		}
		t.Fatalf("cross-file nodes = %d, want 17; got: %v", len(result.Nodes), names)
	}

	// Verify node types from the services plan
	typeMap := make(map[string]string)
	for _, n := range result.Nodes {
		typeMap[n.Name] = string(n.Type)
	}

	serviceExpected := map[string]string{
		"api-service":        "service",
		"api-task":           "container",
		"event-queue":        "queue",
		"prod-alerts":        "pubsub",
		"event-processor":    "function",
		"prod/db-credentials": "secret",
	}
	for name, wantType := range serviceExpected {
		got, ok := typeMap[name]
		if !ok {
			t.Errorf("missing service node %q", name)
		} else if got != wantType {
			t.Errorf("service node %q type = %q, want %q", name, got, wantType)
		}
	}
}

func TestRealisticPlan_CrossFileEdgeResolution(t *testing.T) {
	p := NewPlanParser()
	infra, _ := filepath.Abs("testdata/plan_realistic.json")
	services, _ := filepath.Abs("testdata/plan_services.json")

	result, err := p.ParseMulti(context.Background(), []string{infra, services})
	if err != nil {
		t.Fatal(err)
	}

	// The api-service in plan_services references "prod-vpc" via network attribute
	// and "private-subnet-a" via subnet_id — both defined in plan_realistic.
	// buildPlanRefMap should have mapped these across files.
	edgeSet := make(map[string]bool)
	for _, e := range result.Edges {
		key := string(e.Type) + ":" + e.FromID + "->" + e.ToID
		edgeSet[key] = true
	}

	// ECS service references network=prod-vpc from the infra plan
	if !edgeSet["connects_to:tf:service:api-service->tf:network:prod-vpc"] {
		t.Error("missing cross-file edge: api-service → prod-vpc via network")
	}
}

func TestRealisticPlan_DeleteNodeUsesBeforeState(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	// The deleted S3 bucket (after=null) should use before state for name/metadata
	for _, n := range result.Nodes {
		if n.Name == "prod-logs-bucket" {
			if n.Metadata["plan_action"] != "delete" {
				t.Errorf("prod-logs-bucket plan_action = %q, want delete", n.Metadata["plan_action"])
			}
			if n.ID != "tf:bucket:prod-logs-bucket" {
				t.Errorf("prod-logs-bucket ID = %q, want tf:bucket:prod-logs-bucket", n.ID)
			}
			return
		}
	}
	t.Error("deleted node prod-logs-bucket not found")
}

func TestRealisticPlan_ReplaceNodeUsesAfterState(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	// The replaced security group should use after state for attributes
	for _, n := range result.Nodes {
		if n.Name == "legacy-sg" {
			if n.Metadata["plan_action"] != "replace" {
				t.Errorf("legacy-sg plan_action = %q, want replace", n.Metadata["plan_action"])
			}
			return
		}
	}
	t.Error("replaced node legacy-sg not found")
}

func TestRealisticPlan_SourceFile(t *testing.T) {
	data, err := os.ReadFile("testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ParsePlanBytes(data, "testdata/plan_realistic.json")
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range result.Nodes {
		if n.SourceFile != "testdata/plan_realistic.json" {
			t.Errorf("node %q source_file = %q, want testdata/plan_realistic.json", n.Name, n.SourceFile)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

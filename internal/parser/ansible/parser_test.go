package ansible

import (
	"context"
	"strings"
	"testing"

	"github.com/matijazezelj/aib/pkg/models"
)

func TestParse_INIInventory(t *testing.T) {
	p := NewAnsibleParser("")
	result, err := p.Parse(context.Background(), "testdata/inventory.ini")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Nodes) != 3 {
		t.Errorf("nodes = %d, want 3 (web1, web2, db1)", len(result.Nodes))
	}

	nodeMap := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeMap[n.ID] = n
	}

	// Check web1 node
	web1, ok := nodeMap["ansible:vm:web1"]
	if !ok {
		t.Fatal("missing ansible:vm:web1")
	}
	if web1.Type != models.AssetVM {
		t.Errorf("web1 type = %q, want vm", web1.Type)
	}
	if web1.Source != "ansible" {
		t.Errorf("web1 source = %q, want ansible", web1.Source)
	}
	if web1.Metadata["ansible_host"] != "192.168.1.10" {
		t.Errorf("web1 ansible_host = %q, want 192.168.1.10", web1.Metadata["ansible_host"])
	}
}

func TestParse_YAMLInventory(t *testing.T) {
	p := NewAnsibleParser("")
	result, err := p.Parse(context.Background(), "testdata/inventory.yml")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Nodes) != 3 {
		t.Errorf("nodes = %d, want 3", len(result.Nodes))
	}
}

func TestParse_WithPlaybooks(t *testing.T) {
	p := NewAnsibleParser("testdata")
	result, err := p.Parse(context.Background(), "testdata/inventory.ini")
	if err != nil {
		t.Fatal(err)
	}

	// Should have host nodes + container/service nodes from playbooks
	if len(result.Nodes) < 3 {
		t.Errorf("nodes = %d, want >= 3 (hosts + containers)", len(result.Nodes))
	}

	// Check for container nodes from playbook
	nodeIDs := make(map[string]bool)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = true
	}

	if !nodeIDs["ansible:container:webapp"] {
		t.Error("missing ansible:container:webapp")
	}
	if !nodeIDs["ansible:container:redis-cache"] {
		t.Error("missing ansible:container:redis-cache")
	}

	// Check for managed_by edges
	if len(result.Edges) == 0 {
		t.Error("expected edges from playbook, got 0")
	}

	hasWebappEdge := false
	for _, e := range result.Edges {
		if e.FromID == "ansible:container:webapp" && e.Type == models.EdgeManagedBy {
			hasWebappEdge = true
			break
		}
	}
	if !hasWebappEdge {
		t.Error("missing managed_by edge for webapp container")
	}
}

func TestInferProvider(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"ec2-1-2-3-4.compute-1.amazonaws.com", "aws"},
		{"vm-123.googleusercontent.com", "gcp"},
		{"myvm.azure.com", "azure"},
		{"192.168.1.10", "local"},
	}

	for _, tt := range tests {
		h := hostEntry{hostname: tt.host, vars: map[string]string{"ansible_host": tt.host}}
		got := inferProvider(h)
		if got != tt.want {
			t.Errorf("inferProvider(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestBuildHostMetadata(t *testing.T) {
	h := hostEntry{
		hostname: "web1",
		groups:   []string{"webservers", "production"},
		vars:     map[string]string{"ansible_host": "1.2.3.4", "http_port": "80"},
	}

	meta := buildHostMetadata(h)

	if meta["ansible_host"] != "1.2.3.4" {
		t.Errorf("ansible_host = %q", meta["ansible_host"])
	}
	if meta["groups"] == "" {
		t.Error("groups metadata should be set")
	}
}

func TestParse_InferredDependenciesFromInventoryVars(t *testing.T) {
	p := NewAnsibleParser("")
	result, err := p.Parse(context.Background(), "testdata/inventory_deps.ini")
	if err != nil {
		t.Fatal(err)
	}

	nodeMap := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeMap[n.ID] = n
	}

	if _, ok := nodeMap["ansible:vm:web1"]; !ok {
		t.Fatal("missing ansible:vm:web1")
	}
	if _, ok := nodeMap["ansible:vm:web2"]; !ok {
		t.Fatal("missing ansible:vm:web2")
	}
	if _, ok := nodeMap["ansible:vm:db1"]; !ok {
		t.Fatal("missing ansible:vm:db1")
	}

	redisNode, ok := nodeMap["k8s:service:production/redis-svc"]
	if !ok {
		t.Fatal("missing inferred k8s redis service node")
	}
	if redisNode.Type != models.AssetService {
		t.Errorf("redis node type = %q, want service", redisNode.Type)
	}
	if redisNode.Metadata["connection_string"] == "" {
		t.Fatal("expected inferred redis connection_string metadata on service node")
	}
	if !strings.HasPrefix(redisNode.Metadata["connection_string"], "redis://") {
		t.Errorf("redis connection_string = %q, want prefix \"redis://\"", redisNode.Metadata["connection_string"])
	}
	if !strings.Contains(redisNode.Metadata["connection_string"], "svc.cluster.local") {
		t.Errorf("redis connection_string = %q, want to contain svc.cluster.local", redisNode.Metadata["connection_string"])
	}

	dbNode, ok := nodeMap["ansible:database:exampledb@db1"]
	if !ok {
		t.Fatal("missing inferred database node exampledb@db1")
	}
	if dbNode.Type != models.AssetDatabase {
		t.Errorf("database node type = %q, want database", dbNode.Type)
	}
	if dbNode.Metadata["connection_string"] == "" {
		t.Fatal("expected inferred connection_string metadata on database node")
	}
	if dbNode.Metadata["db_host"] != "db1" {
		t.Errorf("db_host metadata = %q, want db1", dbNode.Metadata["db_host"])
	}

	hasEdge := func(from, to string, edgeType models.EdgeType) bool {
		for _, e := range result.Edges {
			if e.FromID == from && e.ToID == to && e.Type == edgeType {
				return true
			}
		}
		return false
	}

	if !hasEdge("ansible:vm:web1", "ansible:database:exampledb@db1", models.EdgeDependsOn) {
		t.Error("missing web1 -> exampledb@db1 depends_on edge")
	}
	if !hasEdge("ansible:vm:web2", "ansible:database:exampledb@db1", models.EdgeDependsOn) {
		t.Error("missing web2 -> exampledb@db1 depends_on edge")
	}
	if !hasEdge("ansible:database:exampledb@db1", "ansible:vm:db1", models.EdgeConnectsTo) {
		t.Error("missing inferred database -> db1 host binding edge")
	}
	if !hasEdge("ansible:vm:web1", "k8s:service:production/redis-svc", models.EdgeConnectsTo) {
		t.Error("missing web1 -> k8s redis connects_to edge")
	}
	if !hasEdge("ansible:vm:web2", "k8s:service:production/redis-svc", models.EdgeConnectsTo) {
		t.Error("missing web2 -> k8s redis connects_to edge")
	}
}

func nodeMap(nodes []models.Node) map[string]models.Node {
	m := make(map[string]models.Node, len(nodes))
	for _, n := range nodes {
		m[n.ID] = n
	}
	return m
}

// Inventory credentials reach aib.db, JSON reports, and /api/v1/graph, and the
// GitHub Action publishes the first two as CI artifacts. No parser output may
// carry the raw password.
func TestParse_InventoryCredentialsAreNotPersisted(t *testing.T) {
	p := NewAnsibleParser("")
	result, err := p.Parse(context.Background(), "testdata/inventory_secrets.ini")
	if err != nil {
		t.Fatal(err)
	}

	secrets := []string{"SuperSecret123", "VaultPass456", "BecomePass789", "TokenAbc000"}

	var dbNode *models.Node
	for i := range result.Nodes {
		for key, v := range result.Nodes[i].Metadata {
			for _, secret := range secrets {
				if strings.Contains(v, secret) {
					t.Errorf("node %s leaked %s via metadata[%q] = %q", result.Nodes[i].ID, secret, key, v)
				}
			}
		}
		if result.Nodes[i].Type == models.AssetDatabase {
			dbNode = &result.Nodes[i]
		}
	}
	for _, e := range result.Edges {
		for key, v := range e.Metadata {
			for _, secret := range secrets {
				if strings.Contains(v, secret) {
					t.Errorf("edge %s leaked %s via metadata[%q] = %q", e.ID, secret, key, v)
				}
			}
		}
	}

	// The host node keeps the var keys — only the values are replaced, so the
	// graph still shows that a credential is configured.
	web1, ok := nodeMap(result.Nodes)["ansible:vm:web1"]
	if !ok {
		t.Fatal("missing ansible:vm:web1")
	}
	if web1.Metadata["ansible_password"] != "REDACTED" {
		t.Errorf("ansible_password = %q, want REDACTED", web1.Metadata["ansible_password"])
	}
	if web1.Metadata["ansible_host"] != "192.168.1.10" {
		t.Errorf("ansible_host = %q, want the non-secret value preserved", web1.Metadata["ansible_host"])
	}

	if dbNode == nil {
		t.Fatal("expected an inferred database node from database_url")
	}
	cs := dbNode.Metadata["connection_string"]
	if cs != "postgres://admin:REDACTED@192.168.1.20:5432/exampledb" {
		t.Errorf("connection_string = %q, want the redacted DSN", cs)
	}
}

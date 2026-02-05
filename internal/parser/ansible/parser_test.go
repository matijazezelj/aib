package ansible

import (
	"context"
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

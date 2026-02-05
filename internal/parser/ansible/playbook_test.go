package ansible

import (
	"context"
	"testing"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
)

func TestParsePlaybookFile(t *testing.T) {
	// Build host map (simulating parsed inventory)
	hostMap := map[string]hostEntry{
		"web1": {hostname: "web1", groups: []string{"webservers"}, vars: map[string]string{}},
		"web2": {hostname: "web2", groups: []string{"webservers"}, vars: map[string]string{}},
		"db1":  {hostname: "db1", groups: []string{"dbservers"}, vars: map[string]string{}},
	}

	result, err := parsePlaybookFile(context.Background(), "testdata/playbook.yml", hostMap, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// Expect container nodes: webapp, redis-cache (webservers), postgres (dbservers)
	// Expect service nodes: nginx (webservers), postgresql (dbservers)
	nodeIDs := make(map[string]bool)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = true
	}

	wantNodes := []string{
		"ansible:container:webapp",
		"ansible:container:redis-cache",
		"ansible:container:postgres",
		"ansible:service:nginx",
		"ansible:service:postgresql",
	}
	for _, id := range wantNodes {
		if !nodeIDs[id] {
			t.Errorf("missing node %s", id)
		}
	}

	// Check edges
	if len(result.Edges) == 0 {
		t.Fatal("expected edges")
	}

	// webapp should have managed_by edges to web1 and web2
	webappEdges := 0
	for _, e := range result.Edges {
		if e.FromID == "ansible:container:webapp" && e.Type == models.EdgeManagedBy {
			webappEdges++
		}
	}
	if webappEdges != 2 {
		t.Errorf("webapp managed_by edges = %d, want 2 (web1, web2)", webappEdges)
	}
}

func TestResolveHostPattern(t *testing.T) {
	hostMap := map[string]hostEntry{
		"web1": {hostname: "web1", groups: []string{"webservers", "production"}, vars: map[string]string{}},
		"web2": {hostname: "web2", groups: []string{"webservers", "production"}, vars: map[string]string{}},
		"db1":  {hostname: "db1", groups: []string{"dbservers", "production"}, vars: map[string]string{}},
	}

	tests := []struct {
		pattern string
		want    int
	}{
		{"all", 3},
		{"*", 3},
		{"web1", 1},
		{"webservers", 2},
		{"dbservers", 1},
		{"production", 3},
		{"webservers,dbservers", 3},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		got := resolveHostPattern(tt.pattern, hostMap)
		if len(got) != tt.want {
			t.Errorf("resolveHostPattern(%q) = %d hosts, want %d", tt.pattern, len(got), tt.want)
		}
	}
}

func TestExtractDockerMetadata(t *testing.T) {
	m := map[string]interface{}{
		"name":           "webapp",
		"image":          "myapp:latest",
		"state":          "started",
		"restart_policy": "always",
		"extra_field":    "ignored",
	}

	meta := extractDockerMetadata(m)
	if meta["name"] != "webapp" {
		t.Errorf("name = %q", meta["name"])
	}
	if meta["image"] != "myapp:latest" {
		t.Errorf("image = %q", meta["image"])
	}
	if _, ok := meta["extra_field"]; ok {
		t.Error("extra_field should not be in metadata")
	}
}

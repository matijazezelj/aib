package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/matijazezelj/aib/pkg/models"
)

func TestParse(t *testing.T) {
	p := NewComposeParser()
	result, err := p.Parse(context.Background(), "testdata/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}

	// 3 services + 2 networks + 3 volumes = 8 nodes
	if len(result.Nodes) != 8 {
		t.Errorf("nodes = %d, want 8", len(result.Nodes))
	}

	nodeMap := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeMap[n.ID] = n
	}

	// Check service nodes
	web := nodeMap["compose:container:web"]
	if web.Type != models.AssetContainer {
		t.Errorf("web type = %q, want container", web.Type)
	}
	if web.Metadata["image"] != "nginx:1.25" {
		t.Errorf("web image = %q, want nginx:1.25", web.Metadata["image"])
	}
	if web.Provider != "docker" {
		t.Errorf("web provider = %q, want docker", web.Provider)
	}
	if web.Source != "compose" {
		t.Errorf("web source = %q, want compose", web.Source)
	}

	api := nodeMap["compose:container:api"]
	if api.Type != models.AssetContainer {
		t.Errorf("api type = %q, want container", api.Type)
	}

	db := nodeMap["compose:container:db"]
	if db.Type != models.AssetContainer {
		t.Errorf("db type = %q, want container", db.Type)
	}

	// Check network nodes
	frontend := nodeMap["compose:network:frontend"]
	if frontend.Type != models.AssetNetwork {
		t.Errorf("frontend type = %q, want network", frontend.Type)
	}

	backend := nodeMap["compose:network:backend"]
	if backend.Type != models.AssetNetwork {
		t.Errorf("backend type = %q, want network", backend.Type)
	}

	// Check volume nodes
	pgdata := nodeMap["compose:volume:pgdata"]
	if pgdata.Type != models.AssetDisk {
		t.Errorf("pgdata type = %q, want disk", pgdata.Type)
	}

	// Check edges
	edgeMap := make(map[string]models.Edge)
	for _, e := range result.Edges {
		key := e.FromID + "->" + string(e.Type) + "->" + e.ToID
		edgeMap[key] = e
	}

	// depends_on: web → api, api → db
	if _, ok := edgeMap["compose:container:web->depends_on->compose:container:api"]; !ok {
		t.Error("missing web -> depends_on -> api edge")
	}
	if _, ok := edgeMap["compose:container:api->depends_on->compose:container:db"]; !ok {
		t.Error("missing api -> depends_on -> db edge")
	}

	// network connections
	if _, ok := edgeMap["compose:container:web->connects_to->compose:network:frontend"]; !ok {
		t.Error("missing web -> connects_to -> frontend edge")
	}
	if _, ok := edgeMap["compose:container:api->connects_to->compose:network:frontend"]; !ok {
		t.Error("missing api -> connects_to -> frontend edge")
	}
	if _, ok := edgeMap["compose:container:api->connects_to->compose:network:backend"]; !ok {
		t.Error("missing api -> connects_to -> backend edge")
	}
	if _, ok := edgeMap["compose:container:db->connects_to->compose:network:backend"]; !ok {
		t.Error("missing db -> connects_to -> backend edge")
	}

	// volume mounts
	if _, ok := edgeMap["compose:container:web->mounts_secret->compose:volume:static"]; !ok {
		t.Error("missing web -> mounts_secret -> static edge")
	}
	if _, ok := edgeMap["compose:container:api->mounts_secret->compose:volume:logs"]; !ok {
		t.Error("missing api -> mounts_secret -> logs edge")
	}
	if _, ok := edgeMap["compose:container:db->mounts_secret->compose:volume:pgdata"]; !ok {
		t.Error("missing db -> mounts_secret -> pgdata edge")
	}
}

func TestSupported(t *testing.T) {
	p := NewComposeParser()

	// Direct file
	if !p.Supported("testdata/docker-compose.yml") {
		t.Error("should support testdata/docker-compose.yml")
	}

	// Directory containing compose file
	if !p.Supported("testdata") {
		t.Error("should support testdata directory containing docker-compose.yml")
	}

	// Non-compose file
	tmpFile := t.TempDir() + "/random.txt"
	_ = os.WriteFile(tmpFile, []byte("hello"), 0o644)
	if p.Supported(tmpFile) {
		t.Error("should not support random.txt")
	}

	// Non-existent
	if p.Supported("/nonexistent/path") {
		t.Error("should not support non-existent path")
	}
}

func TestParseDirectory(t *testing.T) {
	p := NewComposeParser()
	result, err := p.Parse(context.Background(), "testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 8 {
		t.Errorf("nodes from dir = %d, want 8", len(result.Nodes))
	}
}

func TestParseAlternateNames(t *testing.T) {
	p := NewComposeParser()

	for _, name := range []string{"compose.yml", "compose.yaml", "docker-compose.yaml"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src, _ := os.ReadFile("testdata/docker-compose.yml")
			_ = os.WriteFile(filepath.Join(dir, name), src, 0o644)

			if !p.Supported(dir) {
				t.Errorf("should support dir with %s", name)
			}
			result, err := p.Parse(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Nodes) == 0 {
				t.Error("expected nodes from alternate name")
			}
		})
	}
}

func TestName(t *testing.T) {
	p := NewComposeParser()
	if p.Name() != "compose" {
		t.Errorf("name = %q, want compose", p.Name())
	}
}

func TestParseBadYAML(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(":::bad"), 0o644)

	p := NewComposeParser()
	_, err := p.Parse(context.Background(), dir)
	if err == nil {
		t.Error("expected error for bad YAML")
	}
}

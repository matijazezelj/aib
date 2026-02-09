package kubernetes

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewK8sParser(t *testing.T) {
	p := NewK8sParser("")
	if p == nil {
		t.Fatal("NewK8sParser returned nil")
	}
}

func TestK8sParser_Name(t *testing.T) {
	p := NewK8sParser("")
	if got := p.Name(); got != "kubernetes" {
		t.Errorf("Name() = %q, want %q", got, "kubernetes")
	}
}

func TestK8sParser_Supported_YAMLFile(t *testing.T) {
	p := NewK8sParser("")
	if !p.Supported("testdata/manifests.yaml") {
		t.Error("Supported should return true for .yaml file")
	}
}

func TestK8sParser_Supported_YMLFile(t *testing.T) {
	p := NewK8sParser("")
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close() //nolint:errcheck
	if !p.Supported(f.Name()) {
		t.Error("Supported should return true for .yml file")
	}
}

func TestK8sParser_Supported_NonYAML(t *testing.T) {
	p := NewK8sParser("")
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "test.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close() //nolint:errcheck
	if p.Supported(f.Name()) {
		t.Error("Supported should return false for .json file")
	}
}

func TestK8sParser_Supported_Dir(t *testing.T) {
	p := NewK8sParser("")
	if !p.Supported("testdata") {
		t.Error("Supported should return true for directory with YAML files")
	}
}

func TestK8sParser_Supported_EmptyDir(t *testing.T) {
	p := NewK8sParser("")
	dir := t.TempDir()
	if p.Supported(dir) {
		t.Error("Supported should return false for empty directory")
	}
}

func TestK8sParser_Supported_Nonexistent(t *testing.T) {
	p := NewK8sParser("")
	if p.Supported("/nonexistent/path/manifests.yaml") {
		t.Error("Supported should return false for nonexistent path")
	}
}

func TestK8sParser_Parse_File(t *testing.T) {
	p := NewK8sParser("")
	result, err := p.Parse(context.Background(), "testdata/manifests.yaml")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Error("expected nodes from manifests.yaml, got 0")
	}
}

func TestK8sParser_Parse_Dir(t *testing.T) {
	p := NewK8sParser("")

	// Create temp dir with a minimal YAML manifest
	dir := t.TempDir()
	manifest := `apiVersion: v1
kind: Service
metadata:
  name: test-svc
  namespace: default
spec:
  selector:
    app: test
  ports:
    - port: 80
`
	if err := os.WriteFile(filepath.Join(dir, "svc.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := p.Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("Parse dir error: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Error("expected nodes from directory parse, got 0")
	}
}

func TestWalkYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	// Create .yaml, .yml, and .txt files
	for _, name := range []string{"a.yaml", "b.yml", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var files []string
	if err := walkYAMLFiles(dir, &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("walkYAMLFiles found %d files, want 2 (.yaml and .yml)", len(files))
	}
}

func TestK8sParser_Parse_Nonexistent(t *testing.T) {
	p := NewK8sParser("")
	_, err := p.Parse(context.Background(), "/nonexistent/path/manifests.yaml")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

package kubernetes

import (
	"os"
	"testing"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
)

func TestParseManifests_SampleFile(t *testing.T) {
	data, err := os.ReadFile("testdata/manifests.yaml")
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseManifests(data, "testdata/manifests.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	nodeIDs := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = n
	}

	// Expected nodes from manifests.yaml:
	// Namespace: production
	// Secret: db-credentials
	// ConfigMap: app-config
	// Deployment: api-backend, worker
	// StatefulSet: redis
	// Service: api-backend-svc, redis-svc
	// Ingress: api-ingress
	// Certificate: api-cert
	// Auto-created secrets: api-secret, api-tls-cert (from env ref and TLS/volume)
	wantNodes := []string{
		"k8s:namespace:production",
		"k8s:secret:db-credentials",
		"k8s:configmap:app-config",
		"k8s:pod:api-backend",
		"k8s:pod:worker",
		"k8s:pod:redis",
		"k8s:service:api-backend-svc",
		"k8s:service:redis-svc",
		"k8s:ingress:api-ingress",
		"k8s:certificate:api-cert",
		"k8s:secret:api-tls-cert",
	}

	for _, id := range wantNodes {
		if _, ok := nodeIDs[id]; !ok {
			t.Errorf("missing node %s", id)
		}
	}

	if len(result.Nodes) < 11 {
		t.Errorf("nodes = %d, want >= 11", len(result.Nodes))
	}
}

func TestParseManifests_Edges(t *testing.T) {
	data, err := os.ReadFile("testdata/manifests.yaml")
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseManifests(data, "testdata/manifests.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	edgeMap := make(map[string]models.EdgeType)
	for _, e := range result.Edges {
		edgeMap[e.FromID+"->"+e.ToID] = e.Type
	}

	// Service → workload (member_of via label selector)
	if edgeMap["k8s:pod:api-backend->k8s:service:api-backend-svc"] != models.EdgeMemberOf {
		t.Error("missing member_of edge: api-backend -> api-backend-svc")
	}
	if edgeMap["k8s:pod:redis->k8s:service:redis-svc"] != models.EdgeMemberOf {
		t.Error("missing member_of edge: redis -> redis-svc")
	}

	// Ingress → Service (routes_to)
	if edgeMap["k8s:ingress:api-ingress->k8s:service:api-backend-svc"] != models.EdgeRoutesTo {
		t.Error("missing routes_to edge: api-ingress -> api-backend-svc")
	}

	// Ingress → TLS secret (terminates_tls)
	if edgeMap["k8s:ingress:api-ingress->k8s:secret:api-tls-cert"] != models.EdgeTerminatesTLS {
		t.Error("missing terminates_tls edge: api-ingress -> api-tls-cert")
	}

	// Deployment → Secret (mounts_secret via envFrom)
	if edgeMap["k8s:pod:api-backend->k8s:secret:db-credentials"] != models.EdgeMountsSecret {
		t.Error("missing mounts_secret edge: api-backend -> db-credentials")
	}

	// Deployment → ConfigMap (depends_on via envFrom)
	if edgeMap["k8s:pod:api-backend->k8s:configmap:app-config"] != models.EdgeDependsOn {
		t.Error("missing depends_on edge: api-backend -> app-config")
	}

	// Certificate → Secret (depends_on via cert-manager)
	if edgeMap["k8s:certificate:api-cert->k8s:secret:api-tls-cert"] != models.EdgeDependsOn {
		t.Error("missing depends_on edge: api-cert -> api-tls-cert")
	}
}

func TestParseManifests_DeploymentMetadata(t *testing.T) {
	data, err := os.ReadFile("testdata/manifests.yaml")
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseManifests(data, "testdata/manifests.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range result.Nodes {
		if n.ID == "k8s:pod:api-backend" {
			if n.Metadata["kind"] != "Deployment" {
				t.Errorf("kind = %q, want Deployment", n.Metadata["kind"])
			}
			if n.Metadata["replicas"] != "3" {
				t.Errorf("replicas = %q, want 3", n.Metadata["replicas"])
			}
			if n.Metadata["namespace"] != "production" {
				t.Errorf("namespace = %q, want production", n.Metadata["namespace"])
			}
			if n.Metadata["images"] != "mycompany/api:v2.1.0" {
				t.Errorf("images = %q, want mycompany/api:v2.1.0", n.Metadata["images"])
			}
			return
		}
	}
	t.Error("k8s:pod:api-backend not found")
}

func TestLabelsMatch(t *testing.T) {
	tests := []struct {
		name     string
		selector map[string]string
		target   map[string]string
		want     bool
	}{
		{
			"exact match",
			map[string]string{"app": "web"},
			map[string]string{"app": "web", "tier": "frontend"},
			true,
		},
		{
			"no match",
			map[string]string{"app": "web"},
			map[string]string{"app": "api"},
			false,
		},
		{
			"missing label",
			map[string]string{"app": "web", "env": "prod"},
			map[string]string{"app": "web"},
			false,
		},
		{
			"empty selector matches all",
			map[string]string{},
			map[string]string{"app": "anything"},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := labelsMatch(tt.selector, tt.target)
			if got != tt.want {
				t.Errorf("labelsMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseManifests_InvalidYAML(t *testing.T) {
	data := []byte("---\nkind: Deployment\nmetadata:\n  name: test\n---\n{invalid yaml")
	result, err := parseManifests(data, "test.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Should parse valid documents and produce warnings for invalid ones
	if len(result.Warnings) == 0 {
		t.Error("expected warnings for invalid YAML document")
	}
}

func TestParseManifests_UnsupportedKind(t *testing.T) {
	data := []byte(`---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-sa
`)
	result, err := parseManifests(data, "test.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning for unsupported kind")
	}
	if len(result.Nodes) != 0 {
		t.Errorf("nodes = %d, want 0 for unsupported kind", len(result.Nodes))
	}
}

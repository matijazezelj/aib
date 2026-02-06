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
		"k8s:secret:production/db-credentials",
		"k8s:configmap:production/app-config",
		"k8s:pod:production/api-backend",
		"k8s:pod:production/worker",
		"k8s:pod:production/redis",
		"k8s:service:production/api-backend-svc",
		"k8s:service:production/redis-svc",
		"k8s:ingress:production/api-ingress",
		"k8s:certificate:production/api-cert",
		"k8s:secret:production/api-tls-cert",
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
	if edgeMap["k8s:pod:production/api-backend->k8s:service:production/api-backend-svc"] != models.EdgeMemberOf {
		t.Error("missing member_of edge: api-backend -> api-backend-svc")
	}
	if edgeMap["k8s:pod:production/redis->k8s:service:production/redis-svc"] != models.EdgeMemberOf {
		t.Error("missing member_of edge: redis -> redis-svc")
	}

	// Ingress → Service (routes_to)
	if edgeMap["k8s:ingress:production/api-ingress->k8s:service:production/api-backend-svc"] != models.EdgeRoutesTo {
		t.Error("missing routes_to edge: api-ingress -> api-backend-svc")
	}

	// Ingress → TLS secret (terminates_tls)
	if edgeMap["k8s:ingress:production/api-ingress->k8s:secret:production/api-tls-cert"] != models.EdgeTerminatesTLS {
		t.Error("missing terminates_tls edge: api-ingress -> api-tls-cert")
	}

	// Deployment → Secret (mounts_secret via envFrom)
	if edgeMap["k8s:pod:production/api-backend->k8s:secret:production/db-credentials"] != models.EdgeMountsSecret {
		t.Error("missing mounts_secret edge: api-backend -> db-credentials")
	}

	// Deployment → ConfigMap (depends_on via envFrom)
	if edgeMap["k8s:pod:production/api-backend->k8s:configmap:production/app-config"] != models.EdgeDependsOn {
		t.Error("missing depends_on edge: api-backend -> app-config")
	}

	// Certificate → Secret (depends_on via cert-manager)
	if edgeMap["k8s:certificate:production/api-cert->k8s:secret:production/api-tls-cert"] != models.EdgeDependsOn {
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
		if n.ID == "k8s:pod:production/api-backend" {
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
	t.Error("k8s:pod:production/api-backend not found")
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
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
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

func TestParseManifests_RBAC(t *testing.T) {
	data, err := os.ReadFile("testdata/rbac.yaml")
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseManifests(data, "testdata/rbac.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	nodeIDs := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = n
	}

	wantNodes := []string{
		"k8s:serviceaccount:production/app-sa",
		"k8s:role:production/pod-reader",
		"k8s:clusterrole:cluster-admin-role",
		"k8s:rolebinding:production/read-pods",
		"k8s:clusterrolebinding:admin-binding",
		"k8s:networkpolicy:production/deny-all",
		"k8s:job:production/data-migration",
		"k8s:cronjob:production/daily-cleanup",
		"k8s:hpa:production/api-hpa",
	}

	for _, id := range wantNodes {
		if _, ok := nodeIDs[id]; !ok {
			t.Errorf("missing node %s", id)
		}
	}

	// Verify types
	if n, ok := nodeIDs["k8s:serviceaccount:production/app-sa"]; ok {
		if n.Type != models.AssetServiceAccount {
			t.Errorf("ServiceAccount type = %q, want service_account", n.Type)
		}
	}
	if n, ok := nodeIDs["k8s:role:production/pod-reader"]; ok {
		if n.Type != models.AssetIAMPolicy {
			t.Errorf("Role type = %q, want iam_policy", n.Type)
		}
	}
	if n, ok := nodeIDs["k8s:networkpolicy:production/deny-all"]; ok {
		if n.Type != models.AssetFirewallRule {
			t.Errorf("NetworkPolicy type = %q, want firewall_rule", n.Type)
		}
	}
	if n, ok := nodeIDs["k8s:hpa:production/api-hpa"]; ok {
		if n.Type != models.AssetMonitor {
			t.Errorf("HPA type = %q, want monitor", n.Type)
		}
	}
	if n, ok := nodeIDs["k8s:cronjob:production/daily-cleanup"]; ok {
		if n.Metadata["schedule"] != "0 2 * * *" {
			t.Errorf("CronJob schedule = %q, want '0 2 * * *'", n.Metadata["schedule"])
		}
	}
}

func TestParseManifests_RBAC_Edges(t *testing.T) {
	// Combine the main manifests (which have api-backend deployment) with RBAC resources
	mainData, err := os.ReadFile("testdata/manifests.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rbacData, err := os.ReadFile("testdata/rbac.yaml")
	if err != nil {
		t.Fatal(err)
	}
	data := append(mainData, []byte("\n---\n")...)
	data = append(data, rbacData...)

	result, err := parseManifests(data, "test.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	edgeMap := make(map[string]models.EdgeType)
	for _, e := range result.Edges {
		edgeMap[e.FromID+"->"+e.ToID] = e.Type
	}

	// RoleBinding → Role (depends_on via roleRef)
	if edgeMap["k8s:rolebinding:production/read-pods->k8s:role:production/pod-reader"] != models.EdgeDependsOn {
		t.Error("missing depends_on edge: read-pods -> pod-reader")
	}

	// ServiceAccount → RoleBinding (managed_by via subject)
	if edgeMap["k8s:serviceaccount:production/app-sa->k8s:rolebinding:production/read-pods"] != models.EdgeManagedBy {
		t.Error("missing managed_by edge: app-sa -> read-pods")
	}

	// ClusterRoleBinding → ClusterRole
	if edgeMap["k8s:clusterrolebinding:admin-binding->k8s:clusterrole:cluster-admin-role"] != models.EdgeDependsOn {
		t.Error("missing depends_on edge: admin-binding -> cluster-admin-role")
	}

	// NetworkPolicy → Pod (managed_by via podSelector, matching api-backend from manifests.yaml)
	if edgeMap["k8s:pod:production/api-backend->k8s:networkpolicy:production/deny-all"] != models.EdgeManagedBy {
		t.Error("missing managed_by edge: api-backend -> deny-all (via podSelector)")
	}

	// HPA → Deployment (managed_by via scaleTargetRef)
	if edgeMap["k8s:pod:production/api-backend->k8s:hpa:production/api-hpa"] != models.EdgeManagedBy {
		t.Error("missing managed_by edge: api-backend -> api-hpa (via scaleTargetRef)")
	}
}

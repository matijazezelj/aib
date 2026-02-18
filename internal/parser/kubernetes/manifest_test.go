package kubernetes

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
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
	// Auto-created secrets: api-secret (env secretKeyRef), api-tls-cert (volume + TLS/cert-manager)
	// Derived certificate from TLS secret: api-tls-cert
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
		"k8s:certificate:production/api-tls-cert",
		"k8s:secret:production/api-tls-cert",
		"k8s:secret:production/api-secret",
	}

	for _, id := range wantNodes {
		if _, ok := nodeIDs[id]; !ok {
			t.Errorf("missing node %s", id)
		}
	}

	if len(result.Nodes) < 13 {
		t.Errorf("nodes = %d, want >= 13", len(result.Nodes))
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

	// Ingress → derived TLS certificate (terminates_tls)
	if edgeMap["k8s:ingress:production/api-ingress->k8s:certificate:production/api-tls-cert"] != models.EdgeTerminatesTLS {
		t.Error("missing terminates_tls edge: api-ingress -> certificate/api-tls-cert")
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

	// TLS Secret → derived Certificate (depends_on via tls_secret)
	if edgeMap["k8s:secret:production/api-tls-cert->k8s:certificate:production/api-tls-cert"] != models.EdgeDependsOn {
		t.Error("missing depends_on edge: api-tls-cert secret -> certificate/api-tls-cert")
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

func TestParseManifests_TLSSecret_DerivesCertificateNode(t *testing.T) {
	data := []byte("---\n" +
		"apiVersion: v1\n" +
		"kind: Secret\n" +
		"metadata:\n" +
		"  name: mtls-cert\n" +
		"  namespace: production\n" +
		"type: kubernetes.io/tls\n" +
		"data:\n" +
		"  tls.crt: LS0tLQ==\n" +
		"  tls.key: LS0tLQ==\n")

	result, err := parseManifests(data, "tls-secret.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	nodeIDs := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = n
	}

	secretID := "k8s:secret:production/mtls-cert"
	certID := "k8s:certificate:production/mtls-cert"

	if _, ok := nodeIDs[secretID]; !ok {
		t.Fatalf("missing TLS secret node %s", secretID)
	}
	certNode, ok := nodeIDs[certID]
	if !ok {
		t.Fatalf("missing derived certificate node %s", certID)
	}
	if certNode.Type != models.AssetCertificate {
		t.Fatalf("derived node type = %q, want %q", certNode.Type, models.AssetCertificate)
	}
	if certNode.Metadata["derived_from_secret"] != "true" {
		t.Fatalf("derived_from_secret = %q, want true", certNode.Metadata["derived_from_secret"])
	}

	edgeFound := false
	for _, e := range result.Edges {
		if e.FromID == secretID && e.ToID == certID && e.Type == models.EdgeDependsOn {
			edgeFound = true
			break
		}
	}
	if !edgeFound {
		t.Fatalf("missing depends_on edge from %s to %s", secretID, certID)
	}
}

func TestExtractTLSSecretExpiry_FromPEM(t *testing.T) {
	notAfter := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	crtB64 := mustSelfSignedTLSCertBase64(t, notAfter)

	got, err := extractTLSSecretExpiry(map[string]string{"tls.crt": crtB64})
	if err != nil {
		t.Fatalf("extractTLSSecretExpiry returned error: %v", err)
	}
	if got == nil {
		t.Fatalf("extractTLSSecretExpiry returned nil expiry")
	}
	if got.UTC().Format(time.RFC3339) != notAfter.Format(time.RFC3339) {
		t.Fatalf("expiry = %s, want %s", got.UTC().Format(time.RFC3339), notAfter.Format(time.RFC3339))
	}
}

func TestParseManifests_CertManagerCertificate_ExpiryFromStatus(t *testing.T) {
	notAfter := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	data := []byte(fmt.Sprintf(`---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: app-cert
  namespace: production
spec:
  secretName: app-cert-secret
  dnsNames:
    - app.example.internal
status:
  notAfter: %s
`, notAfter.Format(time.RFC3339)))

	result, err := parseManifests(data, "cert-status.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	var certNode *models.Node
	for i := range result.Nodes {
		if result.Nodes[i].ID == "k8s:certificate:production/app-cert" {
			certNode = &result.Nodes[i]
			break
		}
	}
	if certNode == nil {
		t.Fatalf("missing certificate node")
	}
	if certNode.ExpiresAt == nil {
		t.Fatalf("cert-manager certificate expires_at is nil")
	}
	if certNode.ExpiresAt.UTC().Format(time.RFC3339) != notAfter.Format(time.RFC3339) {
		t.Fatalf("cert-manager certificate expires_at = %s, want %s", certNode.ExpiresAt.UTC().Format(time.RFC3339), notAfter.Format(time.RFC3339))
	}
	if certNode.Metadata["not_after"] == "" {
		t.Fatalf("cert-manager certificate metadata.not_after is empty")
	}
}

func mustSelfSignedTLSCertBase64(t *testing.T, notAfter time.Time) string {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{CommonName: "mtls-cert"},
		NotBefore: time.Now().UTC().Add(-1 * time.Hour),
		NotAfter:  notAfter,
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return base64.StdEncoding.EncodeToString(pemBytes)
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

func TestAutoCreateMissingSecretAndConfigMap(t *testing.T) {
	// A Deployment that references secrets and configmaps via all 6 mechanisms,
	// none of which are defined as explicit resources in the manifest.
	manifest := `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: test
spec:
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
        - name: app
          image: myapp:latest
          envFrom:
            - secretRef:
                name: envfrom-secret
            - configMapRef:
                name: envfrom-cm
          env:
            - name: KEY1
              valueFrom:
                secretKeyRef:
                  name: env-secret
                  key: k
            - name: KEY2
              valueFrom:
                configMapKeyRef:
                  name: env-cm
                  key: k
      volumes:
        - name: vol-secret
          secret:
            secretName: vol-secret
        - name: vol-cm
          configMap:
            name: vol-cm
`
	result, err := parseManifests([]byte(manifest), "test.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	nodeIDs := make(map[string]models.Node)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = n
	}

	// All 6 referenced resources should be auto-created as nodes
	wantAutoCreated := []struct {
		id   string
		name string
	}{
		{"k8s:secret:test/vol-secret", "vol-secret"},
		{"k8s:configmap:test/vol-cm", "vol-cm"},
		{"k8s:secret:test/envfrom-secret", "envfrom-secret"},
		{"k8s:configmap:test/envfrom-cm", "envfrom-cm"},
		{"k8s:secret:test/env-secret", "env-secret"},
		{"k8s:configmap:test/env-cm", "env-cm"},
	}

	for _, want := range wantAutoCreated {
		n, ok := nodeIDs[want.id]
		if !ok {
			t.Errorf("missing auto-created node %s", want.id)
			continue
		}
		if n.Name != want.name {
			t.Errorf("node %s: name = %q, want %q", want.id, n.Name, want.name)
		}
		if n.Metadata["auto_created"] != "true" {
			t.Errorf("node %s: missing auto_created metadata", want.id)
		}
		if n.Metadata["namespace"] != "test" {
			t.Errorf("node %s: namespace = %q, want test", want.id, n.Metadata["namespace"])
		}
	}

	// Verify edges also exist for all 6
	edgeMap := make(map[string]models.EdgeType)
	for _, e := range result.Edges {
		edgeMap[e.FromID+"->"+e.ToID] = e.Type
	}

	wlID := "k8s:pod:test/myapp"
	wantEdges := map[string]models.EdgeType{
		wlID + "->k8s:secret:test/vol-secret":     models.EdgeMountsSecret,
		wlID + "->k8s:configmap:test/vol-cm":       models.EdgeDependsOn,
		wlID + "->k8s:secret:test/envfrom-secret":  models.EdgeMountsSecret,
		wlID + "->k8s:configmap:test/envfrom-cm":   models.EdgeDependsOn,
		wlID + "->k8s:secret:test/env-secret":      models.EdgeMountsSecret,
		wlID + "->k8s:configmap:test/env-cm":        models.EdgeDependsOn,
	}

	for key, wantType := range wantEdges {
		if edgeMap[key] != wantType {
			t.Errorf("missing or wrong edge %s: got %v, want %v", key, edgeMap[key], wantType)
		}
	}
}

func TestParseManifests_InferServiceConnectivityFromEnv(t *testing.T) {
	data, err := os.ReadFile("testdata/interconnectivity.yaml")
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseManifests(data, "testdata/interconnectivity.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	connectsTo := make(map[string]bool)
	for _, edge := range result.Edges {
		if edge.Type == models.EdgeConnectsTo {
			connectsTo[edge.FromID+"->"+edge.ToID] = true
		}
	}

	fromID := "k8s:pod:production/api"
	want := []string{
		fromID + "->k8s:service:production/redis-svc",
		fromID + "->k8s:service:production/postgres-svc",
		fromID + "->k8s:service:production/metrics-svc",
	}

	for _, edgeKey := range want {
		if !connectsTo[edgeKey] {
			t.Errorf("missing inferred connects_to edge %s", edgeKey)
		}
	}
}

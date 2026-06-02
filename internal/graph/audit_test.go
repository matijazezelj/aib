package graph

import (
	"context"
	"testing"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
)

func seedAuditStore(t *testing.T) Store {
	t.Helper()
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	nodes := []models.Node{
		{ID: "db:public", Name: "public-db", Type: models.AssetDatabase, Source: "terraform", Metadata: map[string]string{
			"publicly_accessible": "true",
			"storage_encrypted":   "false",
			"deletion_protection": "false",
			"multi_az":            "false",
			"engine":              "mysql",
		}, FirstSeen: now, LastSeen: now},
		{ID: "db:secure", Name: "secure-db", Type: models.AssetDatabase, Source: "terraform", Metadata: map[string]string{
			"publicly_accessible": "false",
			"storage_encrypted":   "true",
			"deletion_protection": "true",
			"multi_az":            "true",
			"engine":              "postgres",
		}, FirstSeen: now, LastSeen: now},
		{ID: "sg:open", Name: "wide-open-sg", Type: models.AssetFirewallRule, Source: "terraform", Metadata: map[string]string{
			"ingress_cidrs": "0.0.0.0/0,::/0",
		}, FirstSeen: now, LastSeen: now},
		{ID: "sg:restricted", Name: "restricted-sg", Type: models.AssetFirewallRule, Source: "terraform", Metadata: map[string]string{
			"ingress_cidrs": "10.0.0.0/8",
		}, FirstSeen: now, LastSeen: now},
		{ID: "bucket:public", Name: "my-public-bucket", Type: models.AssetBucket, Source: "terraform", Metadata: map[string]string{
			"acl": "public-read",
		}, FirstSeen: now, LastSeen: now},
		{ID: "bucket:private", Name: "my-private-bucket", Type: models.AssetBucket, Source: "terraform", Metadata: map[string]string{
			"acl": "private",
		}, FirstSeen: now, LastSeen: now},
		{ID: "vm:public", Name: "public-vm", Type: models.AssetVM, Source: "terraform", Metadata: map[string]string{
			"public_ip":                   "54.100.200.1",
			"associate_public_ip_address": "true",
		}, FirstSeen: now, LastSeen: now},
		{ID: "vm:private", Name: "private-vm", Type: models.AssetVM, Source: "terraform", Metadata: map[string]string{}, FirstSeen: now, LastSeen: now},
		{ID: "pod:privileged", Name: "privileged-app", Type: models.AssetPod, Source: "kubernetes", Metadata: map[string]string{
			"security.host_network":                    "true",
			"security.host_pid":                        "true",
			"security.main.privileged":                 "true",
			"security.main.allow_privilege_escalation": "true",
			"security.main.run_as_non_root":            "false",
			"security.main.read_only_root_fs":          "false",
		}, FirstSeen: now, LastSeen: now},
		{ID: "pod:secure", Name: "secure-app", Type: models.AssetPod, Source: "kubernetes", Metadata: map[string]string{
			"security.app.privileged":                 "false",
			"security.app.allow_privilege_escalation": "false",
			"security.app.run_as_non_root":            "true",
			"security.app.read_only_root_fs":          "true",
		}, FirstSeen: now, LastSeen: now},
		{ID: "svc:lb", Name: "public-lb", Type: models.AssetService, Source: "kubernetes", Metadata: map[string]string{
			"service_type": "LoadBalancer",
		}, FirstSeen: now, LastSeen: now},
		{ID: "svc:internal", Name: "internal-svc", Type: models.AssetService, Source: "kubernetes", Metadata: map[string]string{
			"service_type": "ClusterIP",
		}, FirstSeen: now, LastSeen: now},
		{ID: "secret:orphan", Name: "orphan-secret", Type: models.AssetSecret, Source: "kubernetes", Metadata: map[string]string{}, FirstSeen: now, LastSeen: now},
		{ID: "secret:mounted", Name: "mounted-secret", Type: models.AssetSecret, Source: "kubernetes", Metadata: map[string]string{}, FirstSeen: now, LastSeen: now},
	}

	edges := []models.Edge{
		{ID: "pod:secure->mounts_secret->secret:mounted", FromID: "pod:secure", ToID: "secret:mounted", Type: models.EdgeMountsSecret},
	}

	for _, n := range nodes {
		if err := store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert node %s: %v", n.ID, err)
		}
	}
	for _, e := range edges {
		if err := store.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("upsert edge %s: %v", e.ID, err)
		}
	}

	return store
}

func TestRunAudit(t *testing.T) {
	store := seedAuditStore(t)
	ctx := context.Background()

	report, err := RunAudit(ctx, store)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}

	if report.Summary.Total == 0 {
		t.Fatal("expected findings, got 0")
	}

	// Count findings by rule
	ruleCount := make(map[string]int)
	for _, f := range report.Findings {
		ruleCount[f.Rule]++
	}

	// Public database
	if ruleCount["public-database"] != 1 {
		t.Errorf("expected 1 public-database finding, got %d", ruleCount["public-database"])
	}

	// Unencrypted storage
	if ruleCount["unencrypted-storage"] != 1 {
		t.Errorf("expected 1 unencrypted-storage finding, got %d", ruleCount["unencrypted-storage"])
	}

	// Permissive firewall (only wide-open-sg should match)
	if ruleCount["permissive-firewall"] != 1 {
		t.Errorf("expected 1 permissive-firewall finding, got %d", ruleCount["permissive-firewall"])
	}

	// No deletion protection
	if ruleCount["no-deletion-protection"] != 1 {
		t.Errorf("expected 1 no-deletion-protection finding, got %d", ruleCount["no-deletion-protection"])
	}

	// Single-AZ database
	if ruleCount["single-az-database"] != 1 {
		t.Errorf("expected 1 single-az-database finding, got %d", ruleCount["single-az-database"])
	}

	// Public bucket
	if ruleCount["public-bucket"] != 1 {
		t.Errorf("expected 1 public-bucket finding, got %d", ruleCount["public-bucket"])
	}

	// Privileged container
	if ruleCount["privileged-container"] != 1 {
		t.Errorf("expected 1 privileged-container finding, got %d", ruleCount["privileged-container"])
	}

	// Host namespace (host_network + host_pid)
	if ruleCount["host-namespace"] < 2 {
		t.Errorf("expected at least 2 host-namespace findings, got %d", ruleCount["host-namespace"])
	}

	// LoadBalancer service
	if ruleCount["public-load-balancer"] != 1 {
		t.Errorf("expected 1 public-load-balancer finding, got %d", ruleCount["public-load-balancer"])
	}

	// Orphan secret (orphan-secret not mounted, mounted-secret is mounted)
	if ruleCount["orphan-secret"] != 1 {
		t.Errorf("expected 1 orphan-secret finding, got %d", ruleCount["orphan-secret"])
	}

	// Public instance
	if ruleCount["public-instance"] != 1 {
		t.Errorf("expected 1 public-instance finding, got %d", ruleCount["public-instance"])
	}

	// Allow privilege escalation
	if ruleCount["allow-privilege-escalation"] != 1 {
		t.Errorf("expected 1 allow-privilege-escalation finding, got %d", ruleCount["allow-privilege-escalation"])
	}

	// Run as root
	if ruleCount["run-as-root"] != 1 {
		t.Errorf("expected 1 run-as-root finding, got %d", ruleCount["run-as-root"])
	}

	// Writable root fs
	if ruleCount["writable-root-fs"] != 1 {
		t.Errorf("expected 1 writable-root-fs finding, got %d", ruleCount["writable-root-fs"])
	}

	// Check severity counts
	if report.Summary.Critical == 0 {
		t.Error("expected critical findings")
	}
	if report.Summary.Warning == 0 {
		t.Error("expected warning findings")
	}
	if report.Summary.Info == 0 {
		t.Error("expected info findings")
	}

	// Secure resources should NOT generate findings
	for _, f := range report.Findings {
		if f.ResourceID == "db:secure" {
			t.Errorf("secure-db should not have findings, got: %s", f.Description)
		}
		if f.ResourceID == "sg:restricted" {
			t.Errorf("restricted-sg should not have findings, got: %s", f.Description)
		}
		if f.ResourceID == "bucket:private" {
			t.Errorf("private-bucket should not have findings, got: %s", f.Description)
		}
		if f.ResourceID == "vm:private" {
			t.Errorf("private-vm should not have findings, got: %s", f.Description)
		}
		if f.ResourceID == "svc:internal" {
			t.Errorf("internal-svc should not have findings, got: %s", f.Description)
		}
		if f.ResourceID == "secret:mounted" {
			t.Errorf("mounted-secret should not have findings, got: %s", f.Description)
		}
	}
}

func TestContainerOperationalAuditChecks(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	nodes := []models.Node{
		{ID: "compose:container:api", Name: "api", Type: models.AssetContainer, Source: "compose", Metadata: map[string]string{
			"image": "ghcr.io/example/api:latest",
			"ports": "8080:8080",
		}, FirstSeen: now, LastSeen: now},
		{ID: "compose:container:worker", Name: "worker", Type: models.AssetContainer, Source: "compose", Metadata: map[string]string{
			"image":       "ghcr.io/example/worker@sha256:abcdef",
			"healthcheck": "true",
			"init":        "true",
		}, FirstSeen: now, LastSeen: now},
	}

	ruleCount := make(map[string]int)
	for _, check := range []AuditCheck{checkMutableContainerImages, checkComposeInitForLongRunningServices, checkExposedServiceHealthchecks} {
		for _, finding := range check(ctx, nodes, nil) {
			ruleCount[finding.Rule]++
		}
	}

	if ruleCount["mutable-container-image"] != 1 {
		t.Errorf("expected 1 mutable-container-image finding, got %d", ruleCount["mutable-container-image"])
	}
	if ruleCount["compose-missing-init"] != 1 {
		t.Errorf("expected 1 compose-missing-init finding, got %d", ruleCount["compose-missing-init"])
	}
	if ruleCount["exposed-service-no-healthcheck"] != 1 {
		t.Errorf("expected 1 exposed-service-no-healthcheck finding, got %d", ruleCount["exposed-service-no-healthcheck"])
	}
}

func TestImageUsesLatestTag(t *testing.T) {
	tests := []struct {
		image string
		want  bool
	}{
		{"nginx", true},
		{"nginx:latest", true},
		{"ghcr.io/org/app:latest", true},
		{"ghcr.io/org/app:1.2.3", false},
		{"registry:5000/app:1.2.3", false},
		{"ghcr.io/org/app@sha256:abcdef", false},
	}
	for _, tt := range tests {
		if got := imageUsesLatestTag(tt.image); got != tt.want {
			t.Errorf("imageUsesLatestTag(%q) = %v, want %v", tt.image, got, tt.want)
		}
	}
}

func TestRunAuditEmptyStore(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	report, err := RunAudit(ctx, store)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	if report.Summary.Total != 0 {
		t.Errorf("expected 0 findings on empty store, got %d", report.Summary.Total)
	}
}

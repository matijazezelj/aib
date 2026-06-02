package graph

import (
	"context"
	"fmt"
	"strings"

	"github.com/matijazezelj/aib/pkg/models"
)

// Severity indicates how critical a finding is.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// Finding represents a single security audit finding.
type Finding struct {
	Severity    Severity `json:"severity"`
	Rule        string   `json:"rule"`
	ResourceID  string   `json:"resource_id"`
	Resource    string   `json:"resource"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Title       string   `json:"title"` // alias for Description — for API consumers that expect title
}

// AuditReport is the result of a full security audit.
type AuditReport struct {
	Findings []Finding    `json:"findings"`
	Summary  AuditSummary `json:"summary"`
}

// AuditSummary provides counts by severity.
type AuditSummary struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
}

// FilterByNode returns a new AuditReport containing only findings for the given node ID.
func (r *AuditReport) FilterByNode(nodeID string) *AuditReport {
	var findings []Finding
	for _, f := range r.Findings {
		if f.ResourceID == nodeID {
			findings = append(findings, f)
		}
	}
	summary := AuditSummary{Total: len(findings)}
	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			summary.Critical++
		case SeverityWarning:
			summary.Warning++
		case SeverityInfo:
			summary.Info++
		}
	}
	return &AuditReport{Findings: findings, Summary: summary}
}

// AuditCheck is a function that inspects nodes/edges and returns findings.
type AuditCheck func(ctx context.Context, nodes []models.Node, edges []models.Edge) []Finding

// RunAudit runs all built-in security checks against the store.
func RunAudit(ctx context.Context, store Store) (*AuditReport, error) {
	nodes, err := store.ListNodes(ctx, NodeFilter{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	edges, err := store.ListEdges(ctx, EdgeFilter{})
	if err != nil {
		return nil, fmt.Errorf("list edges: %w", err)
	}

	checks := []AuditCheck{
		checkPublicDatabases,
		checkUnencryptedStorage,
		checkPermissiveFirewallRules,
		checkMissingDeletionProtection,
		checkSingleAZDatabases,
		checkPublicBuckets,
		checkPrivilegedContainers,
		checkHostNamespaceContainers,
		checkLoadBalancerServices,
		checkOrphanSecrets,
		checkPublicInstances,
		checkContainerSecurityBestPractices,
		checkUnencryptedIngress,
		checkMissingContainerResources,
		checkAbsentEncryption,
		checkMutableContainerImages,
		checkComposeInitForLongRunningServices,
		checkExposedServiceHealthchecks,
	}

	var all []Finding
	for _, check := range checks {
		all = append(all, check(ctx, nodes, edges)...)
	}

	// Populate Title as a mirror of Description for API consumers that expect a title field.
	for i := range all {
		all[i].Title = all[i].Description
	}

	report := &AuditReport{
		Findings: all,
		Summary: AuditSummary{
			Total: len(all),
		},
	}
	for _, f := range all {
		switch f.Severity {
		case SeverityCritical:
			report.Summary.Critical++
		case SeverityWarning:
			report.Summary.Warning++
		case SeverityInfo:
			report.Summary.Info++
		}
	}

	return report, nil
}

// --- Individual audit checks ---

// checkPublicDatabases flags databases with publicly_accessible=true.
func checkPublicDatabases(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetDatabase {
			continue
		}
		if metaTrue(n.Metadata, "publicly_accessible", "PubliclyAccessible") {
			findings = append(findings, Finding{
				Severity:    SeverityCritical,
				Rule:        "public-database",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: "Database is publicly accessible",
			})
		}
	}
	return findings
}

// checkUnencryptedStorage flags databases and buckets without encryption.
func checkUnencryptedStorage(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	encryptionTypes := map[models.AssetType]bool{
		models.AssetDatabase: true,
		models.AssetBucket:   true,
		models.AssetDisk:     true,
	}
	for _, n := range nodes {
		if !encryptionTypes[n.Type] {
			continue
		}
		// If any encryption key is present and explicitly false, flag it
		enc := metaValue(n.Metadata, "encrypted", "storage_encrypted", "StorageEncrypted")
		if enc == "false" {
			findings = append(findings, Finding{
				Severity:    SeverityCritical,
				Rule:        "unencrypted-storage",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: fmt.Sprintf("%s storage is not encrypted", n.Type),
			})
		}
	}
	return findings
}

// checkPermissiveFirewallRules flags security groups with 0.0.0.0/0 ingress CIDRs.
func checkPermissiveFirewallRules(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetFirewallRule {
			continue
		}
		cidrs := metaValue(n.Metadata, "ingress_cidrs")
		if cidrs == "" {
			continue
		}
		for _, cidr := range strings.Split(cidrs, ",") {
			cidr = strings.TrimSpace(cidr)
			if cidr == "0.0.0.0/0" || cidr == "::/0" {
				findings = append(findings, Finding{
					Severity:    SeverityCritical,
					Rule:        "permissive-firewall",
					ResourceID:  n.ID,
					Resource:    n.Name,
					Type:        string(n.Type),
					Description: fmt.Sprintf("Security group allows ingress from %s (world-open)", cidr),
				})
				break
			}
		}
	}
	return findings
}

// checkMissingDeletionProtection flags databases without deletion protection.
func checkMissingDeletionProtection(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetDatabase {
			continue
		}
		dp := metaValue(n.Metadata, "deletion_protection", "DeletionProtection", "deletionProtection")
		if dp == "false" {
			findings = append(findings, Finding{
				Severity:    SeverityWarning,
				Rule:        "no-deletion-protection",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: "Database has deletion protection disabled",
			})
		}
	}
	return findings
}

// checkSingleAZDatabases flags databases that are not multi-AZ.
func checkSingleAZDatabases(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetDatabase {
			continue
		}
		maz := metaValue(n.Metadata, "multi_az", "MultiAZ", "multiAz")
		if maz == "false" {
			findings = append(findings, Finding{
				Severity:    SeverityWarning,
				Rule:        "single-az-database",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: "Database is not deployed across multiple availability zones",
			})
		}
	}
	return findings
}

// checkPublicBuckets flags S3/GCS buckets with permissive ACLs.
func checkPublicBuckets(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	publicACLs := map[string]bool{
		"public-read":        true,
		"public-read-write":  true,
		"authenticated-read": true,
	}
	for _, n := range nodes {
		if n.Type != models.AssetBucket {
			continue
		}
		acl := metaValue(n.Metadata, "acl", "AccessControl")
		if publicACLs[acl] {
			findings = append(findings, Finding{
				Severity:    SeverityCritical,
				Rule:        "public-bucket",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: fmt.Sprintf("Bucket has permissive ACL: %s", acl),
			})
		}
	}
	return findings
}

// checkPrivilegedContainers flags K8s workloads running privileged containers.
func checkPrivilegedContainers(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetPod {
			continue
		}
		for k, v := range n.Metadata {
			if strings.HasSuffix(k, ".privileged") && v == "true" {
				containerName := extractContainerName(k)
				findings = append(findings, Finding{
					Severity:    SeverityCritical,
					Rule:        "privileged-container",
					ResourceID:  n.ID,
					Resource:    n.Name,
					Type:        string(n.Type),
					Description: fmt.Sprintf("Container %q runs in privileged mode", containerName),
				})
			}
		}
	}
	return findings
}

// checkHostNamespaceContainers flags K8s workloads using host network/PID/IPC.
func checkHostNamespaceContainers(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	hostKeys := []struct {
		key  string
		desc string
	}{
		{"security.host_network", "host network namespace"},
		{"security.host_pid", "host PID namespace"},
		{"security.host_ipc", "host IPC namespace"},
	}
	for _, n := range nodes {
		if n.Type != models.AssetPod {
			continue
		}
		for _, hk := range hostKeys {
			if n.Metadata[hk.key] == "true" {
				findings = append(findings, Finding{
					Severity:    SeverityCritical,
					Rule:        "host-namespace",
					ResourceID:  n.ID,
					Resource:    n.Name,
					Type:        string(n.Type),
					Description: fmt.Sprintf("Pod uses %s", hk.desc),
				})
			}
		}
	}
	return findings
}

// checkLoadBalancerServices flags K8s Services of type LoadBalancer (publicly exposed).
func checkLoadBalancerServices(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetService {
			continue
		}
		if n.Metadata["service_type"] == "LoadBalancer" {
			findings = append(findings, Finding{
				Severity:    SeverityInfo,
				Rule:        "public-load-balancer",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: "Service is exposed via a LoadBalancer (publicly accessible)",
			})
		}
	}
	return findings
}

// checkOrphanSecrets flags secrets not referenced by any workload.
func checkOrphanSecrets(_ context.Context, nodes []models.Node, edges []models.Edge) []Finding {
	// Build set of secret node IDs that are targets of mounts_secret edges
	mountedSecrets := make(map[string]bool)
	for _, e := range edges {
		if e.Type == models.EdgeMountsSecret {
			mountedSecrets[e.ToID] = true
		}
	}

	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetSecret {
			continue
		}
		if !mountedSecrets[n.ID] {
			findings = append(findings, Finding{
				Severity:    SeverityInfo,
				Rule:        "orphan-secret",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: "Secret is not mounted by any workload",
			})
		}
	}
	return findings
}

// checkPublicInstances flags VMs with public IPs.
func checkPublicInstances(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetVM {
			continue
		}
		pubIP := metaValue(n.Metadata, "public_ip", "nat_ip")
		assocPub := metaValue(n.Metadata, "associate_public_ip_address")
		if pubIP != "" || assocPub == "true" {
			findings = append(findings, Finding{
				Severity:    SeverityWarning,
				Rule:        "public-instance",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: "VM instance has a public IP address",
			})
		}
	}
	return findings
}

// checkContainerSecurityBestPractices flags containers missing security hardening.
func checkContainerSecurityBestPractices(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetPod {
			continue
		}
		for k, v := range n.Metadata {
			if strings.HasSuffix(k, ".allow_privilege_escalation") && v == "true" {
				containerName := extractContainerName(k)
				findings = append(findings, Finding{
					Severity:    SeverityWarning,
					Rule:        "allow-privilege-escalation",
					ResourceID:  n.ID,
					Resource:    n.Name,
					Type:        string(n.Type),
					Description: fmt.Sprintf("Container %q allows privilege escalation", containerName),
				})
			}
			if strings.HasSuffix(k, ".run_as_non_root") && v == "false" {
				containerName := extractContainerName(k)
				findings = append(findings, Finding{
					Severity:    SeverityWarning,
					Rule:        "run-as-root",
					ResourceID:  n.ID,
					Resource:    n.Name,
					Type:        string(n.Type),
					Description: fmt.Sprintf("Container %q does not enforce non-root execution", containerName),
				})
			}
			if strings.HasSuffix(k, ".read_only_root_fs") && v == "false" {
				containerName := extractContainerName(k)
				findings = append(findings, Finding{
					Severity:    SeverityInfo,
					Rule:        "writable-root-fs",
					ResourceID:  n.ID,
					Resource:    n.Name,
					Type:        string(n.Type),
					Description: fmt.Sprintf("Container %q has a writable root filesystem", containerName),
				})
			}
		}
	}
	return findings
}

// --- Helpers ---

// metaValue returns the first non-empty value from the metadata map for the given keys.
func metaValue(meta map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := meta[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// metaTrue returns true if any of the given metadata keys is "true".
func metaTrue(meta map[string]string, keys ...string) bool {
	return metaValue(meta, keys...) == "true"
}

// extractContainerName extracts the container name from a metadata key like
// "security.mycontainer.privileged".
func extractContainerName(key string) string {
	parts := strings.SplitN(key, ".", 3)
	if len(parts) >= 2 {
		return parts[1]
	}
	return "unknown"
}

// checkUnencryptedIngress flags Ingress resources without TLS configured.
func checkUnencryptedIngress(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetIngress {
			continue
		}
		hasTLS := metaValue(n.Metadata, "tls", "tls_hosts") != ""
		if !hasTLS {
			findings = append(findings, Finding{
				Severity:    SeverityWarning,
				Rule:        "unencrypted-ingress",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: "Ingress does not have TLS configured",
			})
		}
	}
	return findings
}

// checkMissingContainerResources flags pods/containers without resource
// requests or limits defined.
func checkMissingContainerResources(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetPod {
			continue
		}
		// Look for any container metadata with resource limits.
		hasLimits := false
		for k := range n.Metadata {
			if strings.Contains(k, "resources_limits") || strings.Contains(k, "resources_requests") {
				hasLimits = true
				break
			}
		}
		if !hasLimits {
			findings = append(findings, Finding{
				Severity:    SeverityInfo,
				Rule:        "missing-resource-limits",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: "Pod does not define container resource requests/limits",
			})
		}
	}
	return findings
}

// checkAbsentEncryption flags storage resources where no encryption metadata
// is present at all (rather than just set to false).
func checkAbsentEncryption(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	encryptionTypes := map[models.AssetType]bool{
		models.AssetDatabase: true,
		models.AssetDisk:     true,
	}
	for _, n := range nodes {
		if !encryptionTypes[n.Type] {
			continue
		}
		enc := metaValue(n.Metadata, "encrypted", "storage_encrypted", "StorageEncrypted")
		if enc == "" {
			findings = append(findings, Finding{
				Severity:    SeverityInfo,
				Rule:        "absent-encryption-config",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: fmt.Sprintf("%s has no encryption configuration specified", n.Type),
			})
		}
	}
	return findings
}

// checkMutableContainerImages flags containers that use the implicit or explicit
// latest tag. In CI this catches a very common "works until upstream changes"
// footgun before it becomes archaeology with YAML.
func checkMutableContainerImages(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Type != models.AssetContainer && n.Type != models.AssetPod {
			continue
		}
		image := metaValue(n.Metadata, "image")
		if image == "" {
			continue
		}
		if imageUsesLatestTag(image) {
			findings = append(findings, Finding{
				Severity:    SeverityWarning,
				Rule:        "mutable-container-image",
				ResourceID:  n.ID,
				Resource:    n.Name,
				Type:        string(n.Type),
				Description: fmt.Sprintf("Container image %q uses a mutable latest tag", image),
			})
		}
	}
	return findings
}

func imageUsesLatestTag(image string) bool {
	image = strings.TrimSpace(image)
	if image == "" || strings.Contains(image, "@sha256:") {
		return false
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon <= lastSlash {
		return true
	}
	return image[lastColon+1:] == "latest"
}

// checkComposeInitForLongRunningServices flags Compose services that publish
// ports or have a healthcheck but do not opt into tini via init: true. That is
// where zombie healthcheck children tend to pile up. Ask me how I know.
func checkComposeInitForLongRunningServices(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Source != "compose" || n.Type != models.AssetContainer {
			continue
		}
		if metaValue(n.Metadata, "ports", "healthcheck") == "" {
			continue
		}
		if metaValue(n.Metadata, "init") == "true" {
			continue
		}
		findings = append(findings, Finding{
			Severity:    SeverityInfo,
			Rule:        "compose-missing-init",
			ResourceID:  n.ID,
			Resource:    n.Name,
			Type:        string(n.Type),
			Description: "Compose service is long-running/exposed but does not set init: true",
		})
	}
	return findings
}

// checkExposedServiceHealthchecks nudges users to add health evidence for
// exposed services instead of merely proving that Docker managed to fork a PID.
func checkExposedServiceHealthchecks(_ context.Context, nodes []models.Node, _ []models.Edge) []Finding {
	var findings []Finding
	for _, n := range nodes {
		if n.Source != "compose" || n.Type != models.AssetContainer {
			continue
		}
		if metaValue(n.Metadata, "ports") == "" || metaValue(n.Metadata, "healthcheck") == "true" {
			continue
		}
		findings = append(findings, Finding{
			Severity:    SeverityInfo,
			Rule:        "exposed-service-no-healthcheck",
			ResourceID:  n.ID,
			Resource:    n.Name,
			Type:        string(n.Type),
			Description: "Compose service publishes ports but has no healthcheck evidence",
		})
	}
	return findings
}

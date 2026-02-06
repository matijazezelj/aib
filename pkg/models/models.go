package models

import "time"

// AssetType represents the kind of infrastructure asset.
type AssetType string

// Asset type constants for infrastructure resources.
const (
	AssetVM           AssetType = "vm"
	AssetContainer    AssetType = "container"
	AssetPod          AssetType = "pod"
	AssetService      AssetType = "service"
	AssetIngress      AssetType = "ingress"
	AssetLoadBalancer AssetType = "load_balancer"
	AssetDatabase     AssetType = "database"
	AssetBucket       AssetType = "bucket"
	AssetDNSRecord    AssetType = "dns_record"
	AssetCertificate  AssetType = "certificate"
	AssetSecret       AssetType = "secret"
	AssetFirewallRule AssetType = "firewall_rule"
	AssetNetwork      AssetType = "network"
	AssetSubnet       AssetType = "subnet"
	AssetIPAddress    AssetType = "ip_address"
	AssetNamespace    AssetType = "namespace"
	AssetNode         AssetType = "node"
	AssetQueue          AssetType = "queue"
	AssetPubSub         AssetType = "pubsub"
	AssetIAMBinding     AssetType = "iam_binding"
	AssetIAMPolicy      AssetType = "iam_policy"
	AssetKMSKey         AssetType = "kms_key"
	AssetServiceAccount AssetType = "service_account"
	AssetIAMGroup       AssetType = "iam_group"
	AssetCDN            AssetType = "cdn"
	AssetDisk           AssetType = "disk"
	AssetInstanceGroup  AssetType = "instance_group"
	AssetHealthCheck    AssetType = "health_check"
	AssetBackendService AssetType = "backend_service"
	AssetMonitor        AssetType = "monitor"
)

// EdgeType represents the kind of relationship between assets.
type EdgeType string

// Edge type constants for relationships between assets.
const (
	EdgeDependsOn     EdgeType = "depends_on"
	EdgeRoutesTo      EdgeType = "routes_to"
	EdgeTerminatesTLS EdgeType = "terminates_tls"
	EdgeAuthsWith     EdgeType = "authenticates_with"
	EdgeResolvesTo    EdgeType = "resolves_to"
	EdgeMemberOf      EdgeType = "member_of"
	EdgeMountsSecret  EdgeType = "mounts_secret"
	EdgeExposedBy     EdgeType = "exposed_by"
	EdgeConnectsTo    EdgeType = "connects_to"
	EdgeManagedBy     EdgeType = "managed_by"
)

// Node represents an infrastructure asset in the dependency graph.
type Node struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       AssetType         `json:"type"`
	Source     string            `json:"source"`
	SourceFile string            `json:"source_file"`
	Provider   string            `json:"provider"`
	Metadata   map[string]string `json:"metadata"`
	ExpiresAt  *time.Time        `json:"expires_at,omitempty"`
	LastSeen   time.Time         `json:"last_seen"`
	FirstSeen  time.Time         `json:"first_seen"`
}

// Edge represents a relationship between two nodes.
type Edge struct {
	ID       string            `json:"id"`
	FromID   string            `json:"from_id"`
	ToID     string            `json:"to_id"`
	Type     EdgeType          `json:"type"`
	Metadata map[string]string `json:"metadata"`
}

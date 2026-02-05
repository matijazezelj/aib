# AIB — Assets in a Box

## Project Overview

AIB (Assets in a Box) is a lightweight, self-hosted infrastructure asset discovery and dependency mapping tool. It parses IaC sources (Terraform, Helm/K8s manifests, Ansible), builds a unified asset dependency graph, tracks certificate/secret expiry, and provides blast radius analysis ("what breaks if X fails").

Part of the "in a box" security toolbox alongside [SIB](https://github.com/matijazezelj/sib) (SIEM in a Box) and [NIB](https://github.com/matijazezelj/nib) (NIDS in a Box).

## Design Principles

- **Single binary** — Go, no external runtime dependencies
- **Minimal infrastructure** — SQLite for storage, embedded web server, no mandatory external services
- **Terminal-first** — full functionality available via CLI; web UI is optional visualization layer
- **Composable** — webhook/JSON output for integration with SIB and other tools
- **Container-ready** — single Dockerfile, Helm chart, docker-compose.yml
- **Opinionated defaults** — works out of the box with sensible config, override via YAML

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                        AIB Core                         │
│                                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │   Parsers    │  │ Graph Engine│  │  Cert Tracker   │ │
│  │             │  │             │  │                 │ │
│  │ - Terraform │──▶│  Asset DB   │◀──│ - TLS Prober   │ │
│  │ - Helm/K8s  │  │  (SQLite)   │  │ - Expiry Calc  │ │
│  │ - Ansible   │  │  Adjacency  │  │ - cert-manager  │ │
│  │ - Discovery │  │  Model      │  │                 │ │
│  └─────────────┘  └──────┬──────┘  └─────────────────┘ │
│                          │                              │
│         ┌────────────────┼────────────────┐             │
│         ▼                ▼                ▼             │
│  ┌────────────┐  ┌─────────────┐  ┌──────────────┐    │
│  │  CLI/Query │  │   Web UI    │  │  Alerting    │    │
│  │  Interface │  │  (Embedded) │  │  (Webhooks)  │    │
│  └────────────┘  └─────────────┘  └──────────────┘    │
└─────────────────────────────────────────────────────────┘
```

## Tech Stack

| Component        | Technology                          | Rationale                                      |
| ---------------- | ----------------------------------- | ---------------------------------------------- |
| Language         | Go 1.22+                            | Single binary, consistent with SIB/NIB         |
| Graph storage    | SQLite (via `modernc.org/sqlite`)   | Zero deps, embedded, CGO-free                  |
| Terraform parser | `hashicorp/hcl/v2` + JSON state     | Parse both .tf and .tfstate                    |
| K8s parser       | `k8s.io/client-go`, `sigs.k8s.io/yaml` | Parse manifests, optional live cluster access |
| Helm parser      | `helm.sh/helm/v3/pkg/chart/loader`  | Load and template charts                       |
| Ansible parser   | Custom YAML parser                  | Inventory + playbook parsing                   |
| TLS probing      | Go `crypto/tls` stdlib              | Active cert chain inspection                   |
| Web UI           | Embedded SPA with Cytoscape.js      | Interactive graph, embedded via `embed.FS`      |
| CLI framework    | `spf13/cobra`                       | Consistent CLI UX                              |
| Config           | `spf13/viper`                       | YAML config + env vars + flags                 |
| Logging          | `log/slog`                          | Structured logging, stdlib                     |
| HTTP server      | `net/http` stdlib                   | Embedded, no framework needed                  |

## Directory Structure

```
aib/
├── cmd/
│   └── aib/
│       └── main.go                  # Entrypoint
├── internal/
│   ├── config/
│   │   └── config.go                # Configuration loading (Viper)
│   ├── graph/
│   │   ├── graph.go                 # Core graph types: Node, Edge, Graph
│   │   ├── store.go                 # SQLite storage interface
│   │   ├── store_sqlite.go          # SQLite implementation
│   │   ├── query.go                 # Graph traversal, impact analysis, blast radius
│   │   └── export.go                # DOT, JSON, Mermaid export
│   ├── parser/
│   │   ├── parser.go                # Common parser interface
│   │   ├── terraform/
│   │   │   ├── state.go             # .tfstate JSON parser
│   │   │   ├── hcl.go               # .tf HCL parser for relationships
│   │   │   └── mapper.go            # Map TF resources to graph nodes/edges
│   │   ├── kubernetes/
│   │   │   ├── manifest.go          # Parse K8s YAML manifests
│   │   │   ├── helm.go              # Helm chart template + parse
│   │   │   ├── certmanager.go       # cert-manager Certificate CRD parser
│   │   │   └── mapper.go            # Map K8s resources to graph nodes/edges
│   │   └── ansible/
│   │       ├── inventory.go         # Parse Ansible inventory (INI + YAML)
│   │       ├── playbook.go          # Parse playbooks for managed resources
│   │       └── mapper.go            # Map Ansible resources to graph nodes/edges
│   ├── certs/
│   │   ├── prober.go                # Active TLS endpoint probing
│   │   ├── tracker.go               # Expiry tracking, threshold alerts
│   │   └── sources.go               # Cert discovery from graph nodes
│   ├── alert/
│   │   ├── alerter.go               # Alert interface
│   │   ├── webhook.go               # Generic webhook (SIB integration)
│   │   └── stdout.go                # CLI/stdout alerts
│   ├── server/
│   │   ├── server.go                # HTTP server setup
│   │   ├── api.go                   # REST API handlers (/api/graph, /api/impact, /api/certs)
│   │   └── routes.go                # Route registration
│   └── ui/
│       ├── embed.go                 # go:embed for static assets
│       └── static/
│           ├── index.html           # Single page app shell
│           ├── app.js               # Cytoscape.js graph visualization
│           └── style.css            # Minimal styling
├── pkg/
│   └── models/
│       └── models.go                # Shared types: AssetType, EdgeType enums, Node/Edge structs
├── configs/
│   └── aib.yaml.example             # Example configuration file
├── deploy/
│   ├── Dockerfile                   # Multi-stage build
│   ├── docker-compose.yml           # Quick start
│   └── helm/
│       └── aib/
│           ├── Chart.yaml
│           ├── values.yaml
│           └── templates/
│               ├── deployment.yaml
│               ├── service.yaml
│               └── configmap.yaml
├── docs/
│   ├── architecture.md
│   └── parsers.md
├── go.mod
├── go.sum
├── Makefile
├── LICENSE                          # Apache 2.0 (consistent with SIB/NIB)
└── README.md
```

## Data Model

### Node (Asset)

```go
type AssetType string

const (
    AssetVM             AssetType = "vm"
    AssetContainer      AssetType = "container"
    AssetPod            AssetType = "pod"
    AssetService        AssetType = "service"        // K8s Service
    AssetIngress        AssetType = "ingress"
    AssetLoadBalancer   AssetType = "load_balancer"
    AssetDatabase       AssetType = "database"
    AssetBucket         AssetType = "bucket"
    AssetDNSRecord      AssetType = "dns_record"
    AssetCertificate    AssetType = "certificate"
    AssetSecret         AssetType = "secret"
    AssetFirewallRule   AssetType = "firewall_rule"
    AssetNetwork        AssetType = "network"
    AssetSubnet         AssetType = "subnet"
    AssetIPAddress      AssetType = "ip_address"
    AssetNamespace      AssetType = "namespace"
    AssetNode           AssetType = "node"           // K8s/compute node
    AssetQueue          AssetType = "queue"
    AssetPubSub         AssetType = "pubsub"
)

type Node struct {
    ID          string            `json:"id"`          // Unique: source:type:name (e.g., "tf:vm:web-prod-1")
    Name        string            `json:"name"`
    Type        AssetType         `json:"type"`
    Source      string            `json:"source"`      // "terraform", "kubernetes", "ansible", "probe"
    SourceFile  string            `json:"source_file"` // Which file defined this
    Provider    string            `json:"provider"`    // "gcp", "aws", "cloudflare", "local"
    Metadata    map[string]string `json:"metadata"`    // Flexible key-value (region, zone, image, etc.)
    ExpiresAt   *time.Time        `json:"expires_at"`  // For certs, secrets, tokens, DNS
    LastSeen    time.Time         `json:"last_seen"`
    FirstSeen   time.Time         `json:"first_seen"`
}
```

### Edge (Relationship)

```go
type EdgeType string

const (
    EdgeDependsOn     EdgeType = "depends_on"
    EdgeRoutesTo      EdgeType = "routes_to"
    EdgeTerminatesTLS EdgeType = "terminates_tls"
    EdgeAuthsWith     EdgeType = "authenticates_with"
    EdgeResolvesTo    EdgeType = "resolves_to"
    EdgeMemberOf      EdgeType = "member_of"       // pod member_of service, node member_of cluster
    EdgeMountsSecret  EdgeType = "mounts_secret"
    EdgeExposedBy     EdgeType = "exposed_by"      // pod exposed_by ingress
    EdgeConnectsTo    EdgeType = "connects_to"     // network connectivity
    EdgeManagedBy     EdgeType = "managed_by"      // ansible managed host
)

type Edge struct {
    ID       string            `json:"id"`
    FromID   string            `json:"from_id"`
    ToID     string            `json:"to_id"`
    Type     EdgeType          `json:"type"`
    Metadata map[string]string `json:"metadata"` // port, protocol, etc.
}
```

### SQLite Schema

```sql
CREATE TABLE nodes (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,
    source      TEXT NOT NULL,
    source_file TEXT,
    provider    TEXT,
    metadata    TEXT,  -- JSON
    expires_at  DATETIME,
    last_seen   DATETIME NOT NULL,
    first_seen  DATETIME NOT NULL
);

CREATE TABLE edges (
    id        TEXT PRIMARY KEY,
    from_id   TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    to_id     TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    type      TEXT NOT NULL,
    metadata  TEXT,  -- JSON
    UNIQUE(from_id, to_id, type)
);

CREATE INDEX idx_nodes_type ON nodes(type);
CREATE INDEX idx_nodes_source ON nodes(source);
CREATE INDEX idx_nodes_expires_at ON nodes(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX idx_edges_from ON edges(from_id);
CREATE INDEX idx_edges_to ON edges(to_id);
CREATE INDEX idx_edges_type ON edges(type);

-- Scan history for drift detection
CREATE TABLE scans (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source      TEXT NOT NULL,
    source_path TEXT NOT NULL,
    started_at  DATETIME NOT NULL,
    finished_at DATETIME,
    nodes_found INTEGER DEFAULT 0,
    edges_found INTEGER DEFAULT 0,
    status      TEXT DEFAULT 'running'  -- running, completed, failed
);
```

## Parser Interface

All parsers implement a common interface:

```go
// Parser discovers assets from an IaC source and returns nodes and edges.
type Parser interface {
    // Name returns the parser identifier (e.g., "terraform", "kubernetes").
    Name() string

    // Parse reads the source at the given path and returns discovered nodes and edges.
    // Path can be a file, directory, or URL depending on the parser.
    Parse(ctx context.Context, path string) (*ParseResult, error)

    // Supported returns true if this parser can handle the given path.
    Supported(path string) bool
}

type ParseResult struct {
    Nodes []graph.Node
    Edges []graph.Edge
    // Warnings for resources that couldn't be fully parsed
    Warnings []string
}
```

## CLI Commands

```
aib
├── scan                            # Discover assets from sources
│   ├── aib scan terraform <path>   # Scan .tfstate or .tf directory
│   ├── aib scan kubernetes <path>  # Scan K8s manifests / Helm charts
│   ├── aib scan ansible <path>     # Scan Ansible inventory + playbooks
│   └── aib scan all                # Scan all configured sources
├── graph                           # Query the asset graph
│   ├── aib graph show              # Print full graph summary (node/edge counts by type)
│   ├── aib graph nodes             # List all nodes (--type, --source, --provider filters)
│   ├── aib graph edges             # List all edges (--type, --from, --to filters)
│   ├── aib graph neighbors <id>    # Show direct neighbors of a node
│   └── aib graph export            # Export graph (--format=dot|json|mermaid)
├── impact                          # Blast radius analysis
│   ├── aib impact node <id>        # What breaks if this node fails
│   ├── aib impact cert <id>        # What breaks if this cert expires
│   └── aib impact simulate         # Interactive "what if" mode
├── certs                           # Certificate management
│   ├── aib certs list              # List all tracked certs with expiry
│   ├── aib certs expiring          # Show certs expiring within threshold (--days=30)
│   ├── aib certs probe <host:port> # Manually probe a TLS endpoint
│   └── aib certs check             # Re-probe all known cert endpoints
├── serve                           # Start web UI + API server
│   └── aib serve                   # --listen=:8080, --read-only
├── drift                           # Drift detection
│   └── aib drift check             # Compare current scan vs last scan, report missing/new nodes
└── version
```

## REST API

```
GET  /api/v1/graph                  # Full graph (nodes + edges) as JSON
GET  /api/v1/graph/nodes            # List nodes (?type=&source=&provider=)
GET  /api/v1/graph/nodes/:id        # Single node details
GET  /api/v1/graph/edges            # List edges (?type=)
GET  /api/v1/impact/:nodeId         # Blast radius for node
GET  /api/v1/certs                  # All certificates with expiry info
GET  /api/v1/certs/expiring         # Expiring certs (?days=30)
GET  /api/v1/stats                  # Summary stats (node counts by type, expiring certs, etc.)
POST /api/v1/scan                   # Trigger a scan (body: { "source": "terraform", "path": "..." })
GET  /api/v1/scans                  # Scan history
GET  /healthz                       # Health check
```

## Configuration File

```yaml
# aib.yaml
storage:
  path: "./data/aib.db"              # SQLite database path

sources:
  terraform:
    - path: "/path/to/infra/terraform"
      state_file: "terraform.tfstate"  # Optional: explicit state file
    - path: "/path/to/another/project"
  kubernetes:
    - path: "/path/to/k8s/manifests"
    - helm_chart: "/path/to/helm/chart"
      values_file: "values-prod.yaml"  # Optional
    # Optional: connect to live cluster
    # kubeconfig: "~/.kube/config"
    # context: "prod-cluster"
  ansible:
    - inventory: "/path/to/ansible/inventory"
      playbooks: "/path/to/ansible/playbooks"

certs:
  probe_enabled: true
  probe_interval: "6h"               # How often to re-probe TLS endpoints
  alert_thresholds:                   # Days before expiry to alert
    - 90
    - 60
    - 30
    - 14
    - 7
    - 1

alerts:
  webhook:
    enabled: true
    url: "http://sib:8080/api/v1/events"  # SIB integration
    headers:
      Authorization: "Bearer ${AIB_WEBHOOK_TOKEN}"
  stdout:
    enabled: true

server:
  listen: ":8080"
  read_only: false                    # Disable scan triggers via API

scan:
  schedule: "0 */4 * * *"            # Cron: rescan every 4 hours
  on_startup: true                    # Scan all sources on startup
```

## Terraform Parser Details

### State file parsing (.tfstate)

The Terraform state file is the primary source of truth for deployed resources. Parse `resources[]` array:

```
For each resource in state:
  1. Map resource type to AssetType:
     - google_compute_instance      → vm
     - google_sql_database_instance → database
     - google_storage_bucket        → bucket
     - google_compute_network       → network
     - google_compute_subnetwork    → subnet
     - google_compute_address       → ip_address
     - google_compute_firewall      → firewall_rule
     - google_dns_record_set        → dns_record
     - google_compute_forwarding_rule → load_balancer
     - kubernetes_namespace         → namespace
     - tls_certificate / acme_certificate → certificate
     - cloudflare_record            → dns_record
     - (similarly for AWS: aws_instance → vm, aws_rds_instance → database, etc.)

  2. Extract metadata from resource attributes:
     - region, zone, machine_type, image, IP addresses, tags, labels

  3. Discover edges from attribute references:
     - network/subnetwork references → connects_to edges
     - service_account references → authenticates_with edges
     - depends_on → depends_on edges
     - target_tags on firewall rules → connects_to the matching VMs
```

### HCL parsing (.tf files)

Used to supplement state with explicit dependency relationships and resource definitions that may not be in state:

- Parse `depends_on` blocks for explicit dependencies
- Parse resource references (e.g., `google_compute_instance.web.network_interface[0].network`) for implicit edges
- Parse `data` sources for external resource references
- Parse variables and locals for parameterized resource names

## Kubernetes Parser Details

### Manifest parsing

```
For each K8s resource:
  - Deployment/StatefulSet/DaemonSet:
    → Node(type=pod) for the workload
    → edges to Secrets, ConfigMaps via volume mounts / envFrom (mounts_secret)
    → edges to ServiceAccounts (authenticates_with)

  - Service:
    → Node(type=service)
    → edges to Pods via label selector (member_of, reversed)
    → expose port info in metadata

  - Ingress:
    → Node(type=ingress)
    → edges to Services via backend rules (routes_to)
    → edges to TLS secrets (terminates_tls)
    → edge to Certificate if cert-manager annotation present

  - Certificate (cert-manager CRD):
    → Node(type=certificate, expires_at from status.notAfter)
    → edge to Secret where cert is stored
    → edge to Ingress that uses it

  - NetworkPolicy:
    → edges between namespaces/pods for allowed traffic (connects_to)

  - Secret:
    → Node(type=secret)
    → if type=kubernetes.io/tls, extract cert expiry from data
```

### Helm chart parsing

- Use Helm SDK to template the chart with provided values file
- Parse the rendered manifests using the same K8s manifest parser
- Tag all resulting nodes with source_file pointing to the Helm chart

## Ansible Parser Details

### Inventory parsing

- Parse INI-style and YAML inventories
- Create nodes for each host (type=vm or node)
- Create group membership edges (member_of)
- Extract host variables as metadata (ansible_host → IP, ansible_user, etc.)

### Playbook parsing (best-effort)

- Identify common modules that manage infrastructure:
  - `apt/yum/dnf` → package metadata on host nodes
  - `copy/template` → file references in metadata
  - `docker_container` → container nodes managed by host
  - `k8s` module → K8s resources managed by Ansible
  - `gcp_compute`/`aws_ec2` → cloud resources (cross-reference with Terraform)
- Create `managed_by` edges between resources and the Ansible host that manages them

## Graph Query / Impact Analysis

### Blast radius algorithm

```
function blast_radius(start_node_id, direction="downstream"):
    visited = set()
    queue = [start_node_id]
    impact_tree = {}

    while queue:
        current = queue.pop(0)
        if current in visited:
            continue
        visited.add(current)

        neighbors = get_edges(current, direction)
        for edge in neighbors:
            target = edge.to_id if direction == "downstream" else edge.from_id
            impact_tree[target] = {
                "path_from_root": reconstruct_path(start_node_id, target),
                "edge_type": edge.type,
                "depth": depth(target)
            }
            queue.append(target)

    return {
        "root": start_node_id,
        "affected_nodes": len(visited) - 1,
        "impact_tree": impact_tree,
        "affected_by_type": group_by_type(impact_tree)
    }
```

### CLI output example

```
$ aib impact node tf:database:cloudsql-prod

🎯 Impact Analysis: tf:database:cloudsql-prod
   Type: database | Provider: gcp | Source: terraform

   Blast Radius: 7 affected assets

   tf:database:cloudsql-prod
   ├── [depends_on] k8s:pod:api-backend (pod)
   │   ├── [member_of] k8s:service:api-backend-svc (service)
   │   │   ├── [routes_to] k8s:ingress:api-ingress (ingress)
   │   │   │   ├── [terminates_tls] k8s:certificate:api-cert (certificate) ⚠️  expires in 23d
   │   │   │   └── [resolves_to] tf:dns:api.example.com (dns_record)
   │   │   └── [routes_to] k8s:ingress:internal-ingress (ingress)
   │   └── [member_of] k8s:service:api-grpc-svc (service)
   └── [depends_on] k8s:pod:worker (pod)

   ⚠️  Warnings:
   - k8s:certificate:api-cert expires in 23 days
```

## Web UI

Embedded single-page app using Cytoscape.js:

### Features
- Interactive force-directed graph of all assets
- Color-coded nodes by asset type
- Red/amber highlights for expiring certificates
- Click a node to see details panel + blast radius highlight
- Filter by source (Terraform/K8s/Ansible), type, provider
- Search nodes by name
- Cert expiry timeline view (simple bar chart showing upcoming expirations)

### Implementation
- All static assets embedded via `//go:embed` in Go binary
- Single `index.html` with Cytoscape.js loaded from CDN or bundled
- Talks to REST API endpoints for data
- No build step required — vanilla JS, no framework

## SIB Integration

AIB sends events to SIB via webhook:

```json
{
  "source": "aib",
  "event_type": "cert_expiring",
  "severity": "warning",
  "asset": {
    "id": "k8s:certificate:api-cert",
    "name": "api-cert",
    "type": "certificate",
    "expires_at": "2025-04-15T00:00:00Z",
    "days_remaining": 23
  },
  "impact": {
    "affected_count": 5,
    "affected_services": ["api-backend-svc", "api-ingress"]
  },
  "message": "Certificate api-cert expires in 23 days, affecting 5 downstream assets"
}
```

Event types: `cert_expiring`, `cert_expired`, `asset_disappeared` (drift), `new_asset_discovered`, `scan_completed`, `scan_failed`

## Phase 1 MVP — Implementation Order

Build in this order to have a working tool as early as possible:

1. **Project scaffolding** — go.mod, cmd/aib/main.go, cobra CLI setup, config loading
2. **Data model + SQLite store** — schema, CRUD operations, migrations
3. **Graph core** — Node/Edge types, add/query/delete, basic traversal
4. **Terraform state parser** — parse .tfstate, map to nodes/edges, `aib scan terraform`
5. **CLI graph commands** — `aib graph show`, `aib graph nodes`, `aib graph export --format=dot`
6. **Impact analysis** — blast radius algorithm, `aib impact node <id>`
7. **Certificate discovery** — extract certs from graph, TLS endpoint probing, `aib certs list/expiring`
8. **Alerting** — webhook + stdout alerts for cert expiry
9. **Web UI** — embedded Cytoscape.js viewer, REST API
10. **Docker + Helm** — containerize, write Helm chart

## Testing Strategy

- **Unit tests** for each parser with fixture files (sample .tfstate, K8s manifests, Ansible inventories)
- **Integration tests** for SQLite graph store operations
- **Snapshot tests** for graph export formats (DOT, JSON, Mermaid)
- **Test fixtures** in `testdata/` directories alongside each parser package
- Use `testing/fstest.MapFS` for filesystem mocking where needed

## Future Phases (Post-MVP, for reference)

### Phase 2: Kubernetes
- Helm chart parser, K8s manifest parser, cert-manager CRD support
- Live cluster connection option (read-only, via kubeconfig)
- Enrich web UI with K8s-specific views

### Phase 3: Full Picture
- Ansible inventory + playbook parser
- Cross-source node stitching (same VM in Terraform + Ansible → merged node)
- Drift detection (`aib drift check`)

### Phase 4: Extensions
- GCP/AWS API direct discovery (supplement IaC with real state)
- NetworkPolicy visualization
- Cost tracking per asset (from cloud billing APIs)
- Git blame integration (who last changed this resource)
- Scheduled scan via embedded cron

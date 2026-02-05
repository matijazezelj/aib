# AIB — Assets in a Box

Lightweight, self-hosted infrastructure asset discovery and dependency mapping tool. Parses IaC sources (Terraform, Helm/K8s manifests, Ansible), builds a unified asset dependency graph, tracks certificate expiry, and provides blast radius analysis — "what breaks if X fails?"

Part of the "in a box" security toolbox alongside [SIB](https://github.com/matijazezelj/sib) (SIEM in a Box) and [NIB](https://github.com/matijazezelj/nib) (NIDS in a Box).

```
┌─────────────────────────────────────────────────────────┐
│                        AIB Core                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │   Parsers    │  │ Graph Engine│  │  Cert Tracker   │ │
│  │ - Terraform │──▶│  Asset DB   │◀──│ - TLS Prober   │ │
│  │ - Helm/K8s  │  │  (SQLite +  │  │ - Expiry Calc  │ │
│  │ - Ansible   │  │  Memgraph)  │  │                 │ │
│  └─────────────┘  └──────┬──────┘  └─────────────────┘ │
│         ┌────────────────┼────────────────┐             │
│         ▼                ▼                ▼             │
│  ┌────────────┐  ┌─────────────┐  ┌──────────────┐    │
│  │  CLI/Query │  │   Web UI    │  │  Alerting    │    │
│  │  Interface │  │  (Embedded) │  │  (Webhooks)  │    │
│  └────────────┘  └─────────────┘  └──────────────┘    │
└─────────────────────────────────────────────────────────┘
```

## Quick Start

### Build from source

```bash
git clone https://github.com/matijazezelj/aib.git
cd aib
make build
```

### Scan a Terraform state file

```bash
./bin/aib scan terraform /path/to/terraform.tfstate
```

### Scan from a remote backend

```bash
./bin/aib scan terraform /path/to/terraform/project --remote
```

### View the graph

```bash
./bin/aib graph show
./bin/aib graph nodes
./bin/aib graph edges
```

### Analyze blast radius

```bash
./bin/aib impact node tf:network:prod-vpc
```

### Start the web UI

```bash
./bin/aib serve
# Open http://localhost:8080
```

## Installation

**Prerequisites:** Go 1.22+

```bash
# Build
make build

# Or install directly
go install github.com/matijazezelj/aib/cmd/aib@latest
```

### Docker

```bash
# Build image
make docker

# Or use docker-compose (includes Memgraph)
docker compose -f deploy/docker-compose.yml up --build
```

## Usage

### Scanning Sources

Scan Terraform state files to discover infrastructure assets and their dependencies:

```
$ aib scan terraform terraform.tfstate
Scanning Terraform state at terraform.tfstate...
Discovered 6 nodes, 8 edges
```

AIB supports scanning directories containing multiple `.tfstate` files:

```bash
aib scan terraform /path/to/terraform/directory/
```

#### Remote State

Pull state directly from remote backends (S3, GCS, Azure, etc.) using `terraform state pull`:

```bash
# Pull from the current/default workspace
aib scan terraform /path/to/terraform/project --remote

# Pull from a specific workspace
aib scan terraform /path/to/terraform/project --remote --workspace=production

# Pull from all workspaces and merge results
aib scan terraform /path/to/terraform/project --remote --workspace='*'
```

This requires the `terraform` CLI to be installed and the project directory to have a valid backend configuration (e.g. `backend "s3" {}` in your `.tf` files). AIB shells out to `terraform state pull` so your existing credentials and backend config are used as-is.

### Querying the Graph

```
$ aib graph show
Graph Summary
  Total nodes: 6
  Total edges: 8

Nodes by type:
  bucket               1
  database             1
  dns_record           1
  network              1
  subnet               1
  vm                   1

Edges by type:
  connects_to          3
  depends_on           5
```

List nodes with filters:

```bash
aib graph nodes                          # all nodes
aib graph nodes --type=vm                # only VMs
aib graph nodes --source=terraform       # only from Terraform
aib graph nodes --provider=google        # only GCP resources
```

List edges:

```bash
aib graph edges                          # all edges
aib graph edges --type=depends_on        # only dependency edges
aib graph edges --from=tf:vm:web-prod-1  # edges from a specific node
```

Show neighbors of a node:

```bash
aib graph neighbors tf:vm:web-prod-1
```

Export the graph:

```bash
aib graph export --format=json           # JSON (default)
aib graph export --format=dot            # Graphviz DOT
aib graph export --format=mermaid        # Mermaid diagram
```

### Blast Radius Analysis

Analyze what breaks if a node fails:

```
$ aib impact node tf:network:prod-vpc

Impact Analysis: tf:network:prod-vpc
   Type: network | Provider: google | Source: terraform

   Blast Radius: 4 affected assets

   tf:network:prod-vpc (network)
   ├── [connects_to] tf:subnet:prod-subnet (subnet)
   │   └── [connects_to] tf:vm:web-prod-1 (vm)
   │       └── [depends_on] tf:dns_record:web.example.com (dns_record)
   └── [depends_on] tf:database:cloudsql-prod (database)
```

### Certificate Management

Probe a TLS endpoint and track the certificate:

```bash
aib certs probe example.com:443
```

List all tracked certificates:

```bash
aib certs list
```

Show certificates expiring within a threshold:

```bash
aib certs expiring --days=30
```

Re-probe all known endpoints discovered from the graph:

```bash
aib certs check
```

### Web UI and API

Start the embedded web server:

```bash
aib serve                        # default :8080
aib serve --listen=:9090         # custom port
aib serve --read-only            # disable scan triggers via API
```

#### REST API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Health check |
| `GET` | `/api/v1/graph` | Full graph (nodes + edges) |
| `GET` | `/api/v1/graph/nodes` | List nodes (`?type=`, `?source=`, `?provider=`) |
| `GET` | `/api/v1/graph/nodes/:id` | Single node details |
| `GET` | `/api/v1/graph/edges` | List edges (`?type=`, `?from=`, `?to=`) |
| `GET` | `/api/v1/impact/:nodeId` | Blast radius for a node |
| `GET` | `/api/v1/certs` | All tracked certificates |
| `GET` | `/api/v1/certs/expiring` | Expiring certs (`?days=30`) |
| `GET` | `/api/v1/stats` | Summary statistics |
| `GET` | `/api/v1/scans` | Scan history |
| `POST` | `/api/v1/scan` | Trigger a scan |

## Configuration

AIB works out of the box with sensible defaults. For customization, create an `aib.yaml` in the current directory or `~/.aib/`:

```yaml
storage:
  path: "./data/aib.db"
  memgraph:
    enabled: false
    uri: "bolt://localhost:7687"
    username: ""
    password: ""

certs:
  probe_enabled: true
  probe_interval: "6h"
  alert_thresholds: [90, 60, 30, 14, 7, 1]

alerts:
  webhook:
    enabled: false
    url: "http://sib:8080/api/v1/events"
    headers:
      Authorization: "Bearer ${AIB_WEBHOOK_TOKEN}"
  stdout:
    enabled: true

server:
  listen: ":8080"
  read_only: false
```

All settings can also be set via environment variables with the `AIB_` prefix:

```bash
AIB_STORAGE_PATH=/data/aib.db
AIB_STORAGE_MEMGRAPH_ENABLED=true
AIB_STORAGE_MEMGRAPH_URI=bolt://memgraph:7687
AIB_SERVER_LISTEN=:9090
```

See [`configs/aib.yaml.example`](configs/aib.yaml.example) for the full reference.

## Memgraph Integration

AIB uses a **hybrid storage model**: SQLite is the persistent source of truth, and [Memgraph](https://github.com/memgraph/memgraph) is an optional graph traversal engine for faster blast radius analysis, shortest path, and neighbor queries.

### Setup

1. Start Memgraph:

```bash
docker run -p 7687:7687 memgraph/memgraph-mage
```

2. Enable in config (`aib.yaml` or environment):

```yaml
storage:
  memgraph:
    enabled: true
    uri: "bolt://localhost:7687"
```

3. Sync existing data to Memgraph:

```bash
aib graph sync
```

### How it works

- **Writes** go to both SQLite and Memgraph (via `SyncedStore` decorator)
- **Graph traversal queries** (blast radius, neighbors, shortest path) use Memgraph's Cypher engine
- **If Memgraph is unavailable**, all queries fall back to the local BFS engine transparently
- **SQLite stays the source of truth** — Memgraph can be rebuilt at any time with `aib graph sync`

### Docker Compose (with Memgraph)

```bash
docker compose -f deploy/docker-compose.yml up --build
```

This starts both AIB and Memgraph, with AIB automatically configured to use Memgraph for graph queries.

## Supported Resources

### Terraform

AIB maps Terraform resource types to asset types:

| Provider | Resources | Asset Type |
|----------|-----------|------------|
| GCP | `google_compute_instance` | `vm` |
| GCP | `google_sql_database_instance`, `google_redis_instance` | `database` |
| GCP | `google_compute_network` | `network` |
| GCP | `google_compute_subnetwork` | `subnet` |
| GCP | `google_compute_firewall` | `firewall_rule` |
| GCP | `google_dns_record_set` | `dns_record` |
| GCP | `google_storage_bucket` | `bucket` |
| GCP | `google_compute_forwarding_rule` | `load_balancer` |
| AWS | `aws_instance` | `vm` |
| AWS | `aws_db_instance`, `aws_rds_cluster` | `database` |
| AWS | `aws_vpc` | `network` |
| AWS | `aws_subnet` | `subnet` |
| AWS | `aws_security_group` | `firewall_rule` |
| AWS | `aws_route53_record` | `dns_record` |
| AWS | `aws_s3_bucket` | `bucket` |
| AWS | `aws_lb`, `aws_alb`, `aws_elb` | `load_balancer` |
| Azure | `azurerm_virtual_machine` | `vm` |
| Azure | `azurerm_sql_server`, `azurerm_postgresql_server` | `database` |
| Azure | `azurerm_virtual_network` | `network` |
| Cloudflare | `cloudflare_record` | `dns_record` |
| TLS | `tls_self_signed_cert`, `acme_certificate` | `certificate` |

Edges are created from `dependencies` in `.tfstate` and from attribute references (network, subnetwork, vpc_id).

## Development

```bash
make build       # Build binary
make test        # Run tests
make fmt         # Format code
make lint        # Run linter
make clean       # Remove build artifacts
```

## License

Apache 2.0

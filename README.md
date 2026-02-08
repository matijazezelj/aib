[![CI](https://github.com/matijazezelj/aib/actions/workflows/ci.yml/badge.svg)](https://github.com/matijazezelj/aib/actions/workflows/ci.yml)

# AIB — Assets in a Box

![AIB Web UI](assets/aib.png)

Lightweight, self-hosted infrastructure asset discovery and dependency mapping tool. Parses IaC sources (Terraform, CloudFormation, Helm/K8s manifests, Ansible, Docker Compose), builds a unified asset dependency graph, tracks certificate expiry, and provides blast radius analysis — "what breaks if X fails?"

Part of the "in a box" security toolbox alongside [SIB](https://github.com/matijazezelj/sib) (SIEM in a Box) and [NIB](https://github.com/matijazezelj/nib) (NIDS in a Box).

```
┌─────────────────────────────────────────────────────────┐
│                        AIB Core                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │   Parsers    │  │ Graph Engine│  │  Cert Tracker   │ │
│  │ - Terraform │──▶│  Asset DB   │◀──│ - TLS Prober   │ │
│  │ - CloudFmt  │  │  (SQLite +  │  │ - Expiry Calc  │ │
│  │ - Helm/K8s  │  │  Memgraph)  │  │                 │ │
│  │ - Ansible   │  │             │  │                 │ │
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

### Scan Terraform state files

```bash
# Single file or directory (recursive)
./bin/aib scan terraform /path/to/terraform.tfstate
./bin/aib scan terraform /path/to/terraform/directory/

# Multiple paths — cross-state edges resolve automatically
./bin/aib scan terraform networking.tfstate compute.tfstate data.tfstate
./bin/aib scan terraform staging/ production/
```

### Scan from remote backends

```bash
# Multiple remote projects with cross-state resolution
./bin/aib scan terraform --remote project-networking/ project-compute/

# All workspaces across multiple projects
./bin/aib scan terraform --remote --workspace='*' project-a/ project-b/
```

### Scan Kubernetes manifests

```bash
./bin/aib scan k8s /path/to/manifests/
./bin/aib scan k8s base.yaml overlays/prod/ overlays/staging/
./bin/aib scan k8s /path/to/helm/chart --helm
./bin/aib scan k8s /path/to/helm/chart --helm --values=values-prod.yaml
```

### Scan Ansible inventories

```bash
./bin/aib scan ansible /path/to/inventory.ini
./bin/aib scan ansible staging.ini production.ini --playbooks=/path/to/playbooks/
```

### Scan Terraform plan output

```bash
# Parse terraform show -json output for pre-deploy impact analysis
terraform show -json tfplan > plan.json
./bin/aib scan terraform-plan plan.json

# Multiple plan files (cross-file edge resolution)
./bin/aib scan terraform-plan infra-plan.json services-plan.json
```

### Scan CloudFormation templates

```bash
# Single template file (YAML or JSON)
./bin/aib scan cloudformation template.yaml

# Multiple templates (cross-file edge resolution)
./bin/aib scan cloudformation vpc.yaml compute.yaml database.json
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

### Graph analysis

```bash
# Detect circular dependencies
./bin/aib graph cycles

# Find single points of failure (ranked by blast radius)
./bin/aib graph spof
./bin/aib graph spof --min-affected=3 --limit=10

# List orphan nodes (no edges)
./bin/aib graph orphans
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

### Shell Completion

Generate shell completions for tab-completion of commands and flags:

```bash
# Bash
source <(aib completion bash)

# Zsh
aib completion zsh > "${fpath[1]}/_aib"

# Fish
aib completion fish | source

# PowerShell
aib completion powershell | Out-String | Invoke-Expression
```

Run `aib completion --help` for detailed per-shell setup instructions.

## Usage

### Scanning Sources

Scan Terraform state files to discover infrastructure assets and their dependencies:

```
$ aib scan terraform terraform.tfstate
Scanning Terraform state across 1 path(s)...
Discovered 6 nodes, 8 edges
```

AIB recursively discovers `.tfstate` files in directories and supports scanning multiple paths at once. When multiple paths are given, a single global ref map is built so that **cross-state edges resolve automatically** — a VM in one state file that depends on a network defined in another will get proper `depends_on` and `connects_to` edges:

```bash
# Recursive directory scan
aib scan terraform /path/to/terraform/directory/

# Multiple paths with cross-state resolution
aib scan terraform networking/ compute/ data/
aib scan terraform staging.tfstate production.tfstate
```

#### Remote State

Pull state directly from remote backends (S3, GCS, Azure, etc.) using `terraform state pull`. Multiple remote projects are supported with cross-state edge resolution:

```bash
# Single project, default workspace
aib scan terraform /path/to/project --remote

# Specific workspace
aib scan terraform /path/to/project --remote --workspace=production

# All workspaces (cross-workspace resolution)
aib scan terraform /path/to/project --remote --workspace='*'

# Multiple remote projects (cross-state resolution across projects)
aib scan terraform --remote project-networking/ project-compute/ project-data/

# All workspaces across multiple projects
aib scan terraform --remote --workspace='*' project-a/ project-b/
```

This requires the `terraform` CLI to be installed and each project directory to have a valid backend configuration (e.g. `backend "s3" {}` in your `.tf` files). AIB shells out to `terraform state pull` so your existing credentials and backend config are used as-is.

#### Terraform Plan Analysis

Parse `terraform show -json` output to assess the impact of planned changes **before** applying them. AIB classifies each resource change as create, update, delete, or replace, and cross-references with the existing graph to compute blast radius for destructive actions:

```bash
# Generate plan JSON
terraform plan -out=tfplan
terraform show -json tfplan > plan.json

# Scan the plan
aib scan terraform-plan plan.json

# Multiple plan files (cross-file edge resolution)
aib scan terraform-plan infra-plan.json services-plan.json
```

Plan nodes are stored with `source=terraform-plan` and include a `plan_action` metadata field (create/update/delete/replace). No-op resources and data sources are skipped. The plan parser reuses the same 100+ resource type mappings as the state parser, so node IDs are compatible for cross-referencing plan changes against existing state-scanned infrastructure.

The plan impact API (`GET /api/v1/plan/impact`) lists all plan nodes and computes blast radius for update/delete/replace actions.

#### Kubernetes / Helm

Scan Kubernetes YAML manifests or Helm charts to map workloads, services, ingresses, secrets, and their dependencies. Multiple paths are supported:

```bash
# Single manifest file
aib scan k8s deployment.yaml

# Directory of manifests
aib scan k8s /path/to/k8s/manifests/

# Multiple paths
aib scan k8s base.yaml overlays/prod/ overlays/staging/

# Helm chart (renders via helm template, then parses)
aib scan k8s /path/to/chart --helm
aib scan k8s /path/to/chart --helm --values=values-prod.yaml
```

Node IDs are namespace-scoped (e.g. `k8s:pod:production/api-backend`, `k8s:service:default/redis-svc`).

AIB discovers the following relationships:
- **Service → Pod**: label selector matching (`member_of`)
- **Ingress → Service**: backend routing rules (`routes_to`)
- **Ingress → Secret**: TLS termination (`terminates_tls`)
- **Deployment → Secret**: volume mounts, envFrom, env valueFrom (`mounts_secret`)
- **Deployment → ConfigMap**: volume mounts, envFrom (`depends_on`)
- **Certificate → Secret**: cert-manager CRD (`depends_on`)

This enables blast radius queries like "what breaks if the TLS cert secret expires?" — showing the ingress, deployment, and certificate are all affected.

#### Ansible

Scan Ansible inventory files (INI or YAML format) to discover hosts, containers, and services. Multiple paths are supported:

```bash
# INI inventory
aib scan ansible /etc/ansible/hosts

# YAML inventory
aib scan ansible inventory.yml

# Multiple inventories
aib scan ansible staging.ini production.ini

# With playbook analysis (discovers containers, services, and managed_by edges)
aib scan ansible inventory.ini --playbooks=./playbooks/

# Scan a directory containing inventory files
aib scan ansible /path/to/inventory/
```

AIB parses Ansible inventories to discover:
- **Hosts** as VM nodes (`ansible:vm:<hostname>`)
- **Group memberships** and host variables stored as metadata
- **Docker containers** from `docker_container` tasks with `managed_by` edges to target hosts
- **System services** from `service` tasks with `managed_by` edges

Both INI and YAML inventory formats are detected automatically.

#### CloudFormation

Scan AWS CloudFormation templates (YAML or JSON) to discover resources and their dependencies:

```bash
# Single template
aib scan cloudformation template.yaml

# Multiple templates with cross-file edge resolution
aib scan cloudformation vpc.yaml compute.yaml database.json
```

AIB discovers dependencies from:
- **DependsOn** declarations (explicit ordering)
- **Ref** intrinsic functions (parameter/resource references)
- **Fn::GetAtt** intrinsic functions (attribute references)
- **Property references** like VpcId, SubnetId, SecurityGroupIds

Node IDs use the format `cfn:<assetType>:<logicalId>` (e.g. `cfn:vm:WebServer`, `cfn:database:MyDB`). Templates are detected by file extension (`.yaml`/`.yml`/`.json`) and the presence of `AWSTemplateFormatVersion` or `Resources` keys.

### Logging

Control log output format and verbosity with global flags:

```bash
# JSON logging (for log aggregation tools)
aib serve --log-format=json

# Debug level
aib scan terraform ./infra --log-level=debug

# Combined
aib serve --log-format=json --log-level=warn
```

Available formats: `text` (default), `json`
Available levels: `debug`, `info` (default), `warn`, `error`

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

### Graph Analysis

Detect structural issues in your infrastructure graph:

```
$ aib graph cycles
Circular Dependencies
  Cycle 1: A → B → C → A

  Found 1 circular dependency chain(s)
```

```
$ aib graph spof --min-affected=2
Single Points of Failure
  RANK  ID                      NAME       TYPE      AFFECTED
  1     tf:network:prod-vpc     prod-vpc   network   4

  Found 1 single point(s) of failure (min affected: 2)
```

```
$ aib graph orphans
Orphan Nodes (no connections)
  ID                          NAME             TYPE     SOURCE
  tf:bucket:myproj-assets     myproj-assets    bucket   terraform

  Found 1 orphan node(s)
```

**Cycles** indicate circular dependencies that may cause deployment ordering issues. **Single points of failure** are nodes with the highest blast radius — if they fail, many other nodes are affected. **Orphans** are nodes with zero edges, which may indicate stale or misconfigured resources.

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

#### Automatic Certificate Probing

When running `aib serve`, certificates are probed automatically on a schedule. The interval is configured via `certs.probe_interval` (default: `6h`):

```yaml
certs:
  probe_enabled: true
  probe_interval: "6h"    # Go duration: 6h, 30m, 1h30m
  alert_thresholds: [90, 60, 30, 14, 7, 1]
```

The scheduler discovers TLS endpoints from the graph (ingresses, load balancers, DNS records), probes them, and sends alerts via configured backends (webhook, stdout) when certificates are expiring.

### Live Kubernetes Cluster Scanning

Scan a running Kubernetes cluster directly via `kubectl`:

```bash
# Scan all non-system namespaces using default kubeconfig
aib scan k8s --live

# Specify kubeconfig and context
aib scan k8s --live --kubeconfig=~/.kube/config --context=prod-cluster

# Scan specific namespaces only
aib scan k8s --live --namespace=default --namespace=app
```

This requires `kubectl` to be installed and configured with access to the target cluster.

### Web UI and API

Start the embedded web server:

```bash
aib serve                        # default :8080
aib serve --listen=:9090         # custom port
aib serve --read-only            # disable scan triggers via API
```

Interactive API documentation is available at `/api/docs` (Swagger UI) and the OpenAPI 3.0 spec at `/api/v1/openapi.json`.

The web UI provides an interactive graph visualization with:
- Distinct node shapes per asset category (rectangles for compute, diamonds for data, hexagons for networking, etc.)
- Search, filter by type/source, and blast radius highlighting
- A "Scan Now" button to trigger scans from the browser

#### API Authentication

Protect API endpoints with bearer token authentication:

```yaml
server:
  api_token: "${AIB_API_TOKEN}"
```

Or via environment variable:

```bash
AIB_SERVER_API_TOKEN=your-secret-token aib serve
```

Authenticated requests require the `Authorization` header:

```bash
curl -H "Authorization: Bearer your-secret-token" http://localhost:8080/api/v1/stats
```

Authentication applies to `/api/*` routes only. The web UI, static assets, and `/healthz` are always accessible.

#### REST API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Health check |
| `GET` | `/api/v1/graph` | Full graph (nodes + edges) |
| `GET` | `/api/v1/graph/nodes` | List nodes (`?type=`, `?source=`, `?provider=`) |
| `GET` | `/api/v1/graph/nodes/:id` | Single node details |
| `GET` | `/api/v1/graph/edges` | List edges (`?type=`, `?from=`, `?to=`) |
| `GET` | `/api/v1/graph/analysis/cycles` | Detect circular dependencies |
| `GET` | `/api/v1/graph/analysis/spof` | Single points of failure (`?min_affected=`, `?limit=`) |
| `GET` | `/api/v1/graph/analysis/orphans` | Orphan nodes (no edges) |
| `GET` | `/api/v1/impact/:nodeId` | Blast radius for a node |
| `GET` | `/api/v1/plan/impact` | Terraform plan impact analysis |
| `GET` | `/api/v1/certs` | All tracked certificates |
| `GET` | `/api/v1/certs/expiring` | Expiring certs (`?days=30`) |
| `GET` | `/api/v1/stats` | Summary statistics |
| `GET` | `/api/v1/scans` | Scan history |
| `GET` | `/api/v1/scan/status` | Check if a scan is running |
| `POST` | `/api/v1/scan` | Trigger a scan |
| `GET` | `/api/v1/openapi.json` | OpenAPI 3.0 specification |
| `GET` | `/api/docs` | Interactive Swagger UI documentation |

#### Triggering Scans via API

`POST /api/v1/scan` with a JSON body:

```json
{
  "source": "all"
}
```

Valid sources: `terraform`, `terraform-plan`, `kubernetes`, `kubernetes-live`, `ansible`, `compose`, `cloudformation`, `all`. For file-based sources, include `paths`:

```json
{
  "source": "terraform",
  "paths": ["/absolute/path/to/terraform"]
}
```

Returns `202 Accepted` with the scan ID:

```json
{
  "status": "scan triggered",
  "scan_id": 5
}
```

Check scan progress with `GET /api/v1/scan/status`:

```json
{
  "running": true
}
```

#### Security

> **Warning:** AIB is designed for trusted internal networks. Do **not** expose
> to the public internet without a reverse proxy, TLS, and authentication.

The server includes the following security features:

- **Read-only by default**: Server starts in read-only mode. Set `read_only: false` with an `api_token` to enable scan triggers via API
- **Scan path allowlist**: Set `scan.allowed_paths` to restrict which directories the API can scan
- **Security headers**: `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`, `Referrer-Policy` on all responses
- **Rate limiting**: API routes are limited to 10 requests/sec (burst 20) per client IP. Returns `429 Too Many Requests` when exceeded
- **Request body limits**: POST/PUT/PATCH bodies are capped at 1 MB
- **Path validation**: The scan trigger API rejects paths containing `..` (directory traversal) and requires absolute paths
- **CORS**: Disabled by default. Set `server.cors_origin` to enable cross-origin API access
- **Authentication**: Optional bearer token auth on all `/api/*` routes (see above)

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
  probe_interval: "6h"                 # Go duration format
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
  read_only: true                      # set to false + api_token to enable scan triggers
  api_token: "${AIB_API_TOKEN}"        # bearer token for /api/* routes
  cors_origin: ""                      # CORS origin ("*" for any)

scan:
  schedule: "4h"                       # periodic scan interval (Go duration)
  on_startup: true                     # scan all configured sources on startup
  allowed_paths:                       # restrict API-triggered scans to these dirs
    - "/opt/infra/terraform"
    - "/opt/infra/k8s"
```

Sensitive fields (`api_token`, `password`, webhook `url` and `headers`) support `${ENV_VAR}` expansion.

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

Edges are created from `dependencies` in `.tfstate` and from attribute references (network, subnetwork, vpc_id). When scanning multiple state files, cross-state edges are resolved automatically.

### Kubernetes

| Resource Kind | Asset Type | Edges Created |
|--------------|------------|---------------|
| `Deployment`, `StatefulSet`, `DaemonSet` | `pod` | `member_of` Service, `mounts_secret`, `depends_on` ConfigMap |
| `Service` | `service` | matched to Pods via label selector |
| `Ingress` | `ingress` | `routes_to` Service, `terminates_tls` Secret |
| `Secret` | `secret` | referenced by workloads and ingresses |
| `ConfigMap` | `secret` | referenced by workloads |
| `Namespace` | `namespace` | — |
| `Certificate` (cert-manager) | `certificate` | `depends_on` Secret |

Helm charts are supported via `--helm` flag (shells out to `helm template`).

### Ansible

| Source | Discovered Asset | Asset Type |
|--------|-----------------|------------|
| Inventory host | Host machine | `vm` |
| `docker_container` task | Docker container | `container` |
| `service` task | System service | `service` |

Edges (`managed_by`) are created from playbook task analysis, linking containers and services to the hosts they run on.

### CloudFormation

| Resource Type | Asset Type |
|--------------|------------|
| `AWS::EC2::Instance` | `vm` |
| `AWS::RDS::DBInstance`, `AWS::RDS::DBCluster` | `database` |
| `AWS::EC2::VPC` | `network` |
| `AWS::EC2::Subnet` | `subnet` |
| `AWS::EC2::SecurityGroup` | `firewall_rule` |
| `AWS::S3::Bucket` | `bucket` |
| `AWS::Lambda::Function` | `function` |
| `AWS::ECS::Service` | `service` |
| `AWS::ElasticLoadBalancingV2::LoadBalancer` | `load_balancer` |
| `AWS::IAM::Role`, `AWS::IAM::User` | `service_account` |
| `AWS::IAM::Policy` | `iam_policy` |
| `AWS::KMS::Key` | `kms_key` |
| `AWS::SecretsManager::Secret` | `secret` |
| `AWS::SQS::Queue` | `queue` |
| `AWS::SNS::Topic` | `pubsub` |
| `AWS::Route53::RecordSet` | `dns_record` |
| `AWS::CloudFront::Distribution` | `cdn` |
| `AWS::DynamoDB::Table` | `nosql_database` |
| `AWS::CertificateManager::Certificate` | `certificate` |

Edges are created from `DependsOn`, `Ref`, `Fn::GetAtt` intrinsic functions, and property references (`VpcId`, `SubnetId`, `SecurityGroupIds`). When scanning multiple templates, cross-file edges are resolved automatically.

## Known Limitations

- **Not designed for public internet exposure** — intended for trusted internal networks behind a reverse proxy with TLS
- **Single-node only** — no high availability or clustering support
- **No RBAC** — a single shared bearer token protects all API routes
- **SQLite write serialization** — under heavy concurrent write loads, SQLite's single-writer model may become a bottleneck
- **Shell-out dependencies** — `terraform`, `helm`, and `kubectl` must be installed for remote state, Helm chart, and live cluster scanning respectively

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

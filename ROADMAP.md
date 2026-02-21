# AIB Roadmap

## Completed

### v0.1.0 — Core Foundation
- [x] SQLite persistent store with node/edge CRUD
- [x] Memgraph optional graph engine with automatic fallback
- [x] Terraform state parser (GCP, AWS, Azure, Cloudflare — 100+ resource types)
- [x] Kubernetes manifest parser (Deployments, Services, Ingresses, Secrets, ConfigMaps)
- [x] Helm chart support via `helm template`
- [x] Ansible inventory + playbook parser
- [x] Blast radius analysis (upstream BFS)
- [x] CLI: scan, graph show/nodes/edges/neighbors/path, impact
- [x] REST API with embedded Cytoscape.js web UI
- [x] Certificate tracking and TLS probing
- [x] Webhook + stdout alerting

### v0.2.0 — Graph Queries & Compose
- [x] Docker Compose parser (services, networks, volumes, polymorphic depends_on)
- [x] Graph export: JSON, DOT, Mermaid
- [x] Shortest path and dependency chain queries
- [x] Prometheus metrics endpoint (`/metrics`)
- [x] Database backup command
- [x] Cross-state edge resolution for multi-file Terraform scanning
- [x] Live Kubernetes cluster scanning via kubectl

### v0.3.0 — Hardening & DX
- [x] API authentication (bearer token)
- [x] Security headers, rate limiting, request body limits, path validation
- [x] CORS support
- [x] SyncedStore decorator (SQLite + Memgraph dual writes)
- [x] gosec / errcheck / golangci-lint v2 compliance
- [x] CI pipeline (test + lint + gosec) and release pipeline (linux/amd64 + darwin/arm64)

### v0.3.1 — Analysis & Plan Impact
- [x] Graph analysis: cycle detection, single points of failure, orphan nodes
- [x] Terraform plan parser (`terraform show -json` output)
- [x] Plan impact API with blast radius for destructive changes
- [x] Shell completion (bash/zsh/fish/powershell)
- [x] JSON structured logging (`--log-format`, `--log-level`)
- [x] Config validation with multi-error reporting
- [x] Comprehensive test coverage (graph 86%, server 63%, scanner 60%, terraform 66%)

### v0.4.0 — CloudFormation & API Docs
- [x] CloudFormation parser (AWS templates, ~40 resource types, DependsOn/Ref/GetAtt edges)
- [x] OpenAPI 3.0.3 spec at `/api/v1/openapi.json`
- [x] Swagger UI docs at `/api/docs`

### v0.5.0 — Drift Detection
- [x] Scan-time diffing: compares parser output against current store state before upserting
- [x] Source-scoped comparison (terraform scan ignores kubernetes nodes)
- [x] Detects added, removed, and modified nodes; added and removed edges
- [x] Drift summary stored per scan in `scan_diffs` table (lightweight JSON)
- [x] CLI output shows drift after each scan
- [x] API: `GET /api/v1/scans/{id}/diff` returns drift summary

### v0.6.0 — Pulumi Support
- [x] Pulumi state parser (~80 resource types: AWS, GCP, Azure native + classic, Kubernetes, TLS)
- [x] Edge creation from dependencies, attribute refs (vpcId, subnetId, securityGroupIds), parent URNs
- [x] Metadata extraction (instanceType, machineType, vmSize, region, tags, labels, ARN, etc.)
- [x] Two-phase cross-file edge resolution
- [x] Config: `sources.pulumi[].path`
- [x] CLI: `aib scan pulumi <path> [path...]`
- [x] API: `POST /api/v1/scan` accepts `"pulumi"` source

### v0.7.0 — Slack Alerting
- [x] Slack alerter with Block Kit formatted messages (severity colors, emojis, asset details, blast radius)
- [x] Configurable channel override and HTTPS-only webhook URL validation
- [x] Extracted `buildAlerters()` helper to DRY up CLI alerter wiring
- [x] Updated Go version requirement to 1.25.7+

### v1.0.0 — Stable Release
- [x] OpenAPI spec synced to v1.0.0 with 401/429 error responses on all endpoints
- [x] Dockerfile updated to Go 1.25 + Alpine 3.21
- [x] CLI coverage improvement completed (9.7% → 54% via cliApp DI refactor)
- [x] CHANGELOG, SECURITY, and ROADMAP updated for v1.0

### v1.0.1 — JSON Output & Polish
- [x] Global `--output json` (`-o json`) flag for all CLI query commands
- [x] Edge metadata enrichment across all parsers (`via`, `raw_value`, `reference`)
- [x] Connection Evidence section in web UI with filter toggle
- [x] Online Icons toggle (consent-gated, Simple Icons CDN)
- [x] Connection string inference for Ansible (Redis, PostgreSQL)
- [x] Kubernetes workload interconnectivity from env/ConfigMap values
- [x] CSP updated for CDN icon loading (`connect-src`, `img-src data:`)
- [x] Test coverage gaps filled (compose, terraform, pulumi, CFN, server CSP, ansible)
- [x] Microsoft Teams webhook integration

---

## Known Limitations

- **Single-instance only**: no clustering, sharding, or replication — suitable for up to ~10K assets
- **No TLS on serve**: `aib serve` binds plain HTTP; use a reverse proxy (nginx, Caddy) for HTTPS
- **No RBAC**: a single API token grants full access; no per-user or per-team permissions
- **Memgraph optional**: SQLite handles all queries via local BFS; Memgraph accelerates large graphs but is not required
- **Parser coverage is partial**: Terraform covers 100+ types but not all providers; Pulumi ~80 types; CloudFormation ~40 types
- **No audit logging**: API access is not logged beyond standard HTTP request logs

---

## Post-v1.0

### Enhanced Metadata Extraction
Extend `extractMetadata` to handle provider-specific field names:
- AWS RDS `instance_class` (currently only `instance_type` is extracted)
- Azure `sku` fields
- GCP `tier` / `edition` fields

**Priority**: Low — cosmetic improvement, doesn't affect graph structure.

### Future Ideas
- **Graph diff visualization** — side-by-side or overlay in the web UI
- **Policy engine** — define rules like "no orphan nodes in production" or "max blast radius < 10"
- **Multi-tenant support** — separate graphs per team/environment with RBAC
- **Terraform Cloud/Enterprise integration** — pull state directly from TFC API

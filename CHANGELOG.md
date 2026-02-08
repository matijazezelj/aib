# Changelog

All notable changes to AIB are documented here. See [GitHub Releases](https://github.com/matijazezelj/aib/releases) for download links.

## [0.3.1] - 2026-02-08

### Added
- **Graph analysis**: cycle detection (`aib graph cycles`), single points of failure (`aib graph spof`), orphan node discovery (`aib graph orphans`)
- **Terraform plan parser**: parse `terraform show -json` output for pre-deploy impact analysis (`aib scan terraform-plan`)
- **Plan impact API**: `GET /api/v1/plan/impact` — lists plan nodes with blast radius for update/delete/replace actions
- **Graph analysis API**: `/api/v1/graph/analysis/{cycles,spof,orphans}`
- Shell completion for bash, zsh, fish, and powershell (`aib completion`)
- JSON structured logging (`--log-format=json`) and log level control (`--log-level`)
- Configuration validation with multi-error reporting

### Improved
- Graph package test coverage to 86%
- Server package test coverage to 63%

## [0.3.0] - 2026-02-05

### Added
- API authentication with bearer tokens (`server.api_token`)
- Security headers on all responses (CSP, X-Frame-Options, X-Content-Type-Options)
- Rate limiting on API routes (10 req/sec per IP, burst 20)
- Request body size limits (1 MB)
- Path validation on scan trigger API (rejects directory traversal)
- CORS support (`server.cors_origin`)
- SyncedStore decorator for dual SQLite + Memgraph writes

### Improved
- gosec, errcheck, and golangci-lint v2 compliance
- CI pipeline with race detector and dependency verification

## [0.2.0] - 2026-01-28

### Added
- Docker Compose parser (services, networks, volumes)
- Graph export: JSON, DOT, Mermaid formats
- Shortest path and dependency chain queries
- Prometheus metrics endpoint (`/metrics`)
- Database backup command (`aib db backup`)
- Cross-state edge resolution for multi-file Terraform scanning
- Live Kubernetes cluster scanning via kubectl

## [0.1.0] - 2026-01-15

### Added
- Initial release
- SQLite persistent store with Memgraph optional graph engine
- Terraform state parser (GCP, AWS, Azure, Cloudflare — 100+ resource types)
- Kubernetes manifest parser with Helm chart support
- Ansible inventory and playbook parser
- Blast radius analysis
- Certificate tracking and TLS probing
- Webhook and stdout alerting
- Embedded web UI with Cytoscape.js graph visualization
- REST API with full CRUD

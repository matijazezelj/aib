# Changelog

All notable changes to AIB are documented here. See [GitHub Releases](https://github.com/matijazezelj/aib/releases) for download links.

## [0.4.0] - 2026-02-08

### Added
- **CloudFormation parser**: scan AWS CloudFormation templates (YAML/JSON) for resource dependencies (`aib scan cloudformation`)
  - Supports ~40 AWS resource types: EC2, RDS, S3, Lambda, IAM, KMS, VPC, ECS, EKS, SQS, SNS, Route53, CloudFront, and more
  - Edge creation from DependsOn, Ref, Fn::GetAtt intrinsic functions, and property references (VpcId, SubnetId, SecurityGroupIds)
  - Metadata extraction: InstanceType, CidrBlock, Engine, Runtime, BucketName, CFN tags
  - Two-phase parsing with cross-file edge resolution (same pattern as Terraform)
  - Config file support: `sources.cloudformation[].path`
- **OpenAPI spec**: full OpenAPI 3.0.3 specification at `GET /api/v1/openapi.json` covering all 22 API endpoints
- **Swagger UI docs**: interactive API documentation at `/api/docs` powered by Swagger UI from CDN

## [0.3.3] - 2026-02-08

### Added
- **Visible/total counter**: header stats show "X / Y nodes | X / Y edges" that update live on filter, search, and focus changes
- **Edge label controls**: labels hidden by default, revealed on hover/selection; edge-type filter pills toggle edge visibility
- **Focus mode**: upstream/downstream depth controls (1–5) in detail panel, "Secrets Touched" BFS, "External Exposure" path detection
- **Label decluttering**: zoom-based label hiding below threshold; parallel edges fan out with unbundled bezier curves
- **Privacy mode**: masks sensitive node labels (secret, certificate, kms_key), persists in localStorage, keyboard shortcut `p`

## [0.3.2] - 2026-02-08

### Fixed
- **K8s parser**: auto-create missing secret/configmap nodes when referenced by workloads (Deployment, StatefulSet, DaemonSet, ReplicaSet) — fixes FK constraint violations at store time
  - Covers all 6 reference mechanisms: volume secret, volume configmap, envFrom secretRef/configMapRef, env secretKeyRef/configMapKeyRef

### Added
- Web UI screenshot in README (`assets/aib.png`)

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

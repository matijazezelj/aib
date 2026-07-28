# Changelog

All notable changes to AIB are documented here. See [GitHub Releases](https://github.com/matijazezelj/aib/releases) for download links.

## [1.5.0] - 2026-07-28

### Security
- **Credential redaction across all parsers**. Operator-authored key/value pairs were copied verbatim into node metadata, which is persisted to `aib.db`, JSON reports, and `GET /api/v1/graph`.
  - Ansible: every inventory var landed on the host node, including `ansible_password`, `ansible_become_pass`, `ansible_ssh_pass`, and vault tokens. The inferred database node also stored `database_url` / `db_connection_string` / `connection_string` with the DSN password intact.
  - Terraform, CloudFormation, Pulumi: resource tag values leaked (tags named `api_key`, `secret_token`, and similar).
  - Kubernetes: label values leaked across all seven label sites.
  - Values are now redacted by key name and by value shape. URL DSNs, libpq keyword DSNs (`host=x password=y`), and secret-bearing query parameters are all handled; host, port, database, username, and the key names themselves are preserved.
  - Docker Compose was already unaffected, as were the attribute allowlists in Terraform/CloudFormation/Pulumi and Kubernetes Secret `data:`.
- **The GitHub Action no longer uploads `aib.db` by default.** It contains the full asset graph, and on a public repository workflow artifacts are downloadable by anyone. Moved behind a new `upload-database` input, default `false`. Reports continue to upload by default, since baseline diffs depend on them.
- **The Action now verifies the downloaded release binary** against the published `checksums.txt` before making it executable. Previously it executed an unverified download.
- **`GET /api/v1/graph` is capped at 5000 nodes by default.** It previously serialized the entire graph into a single response body (~12 MB at 10k nodes) with a rate-limit burst of 20, making it a cheap memory-amplification vector.

### Fixed
- The Markdown findings table is capped at 100 rows, ordered by severity. It is posted as a PR comment, and GitHub rejects bodies over 65536 characters — 900 findings produced a 97 KB body, which failed the API call and aborted the Action *after* a successful scan. The JSON report is never truncated.
- `GET /api/v1/graph` no longer returns edges whose endpoints fall outside the response, and now reports `total_nodes`, `total_edges`, and `truncated` so a partial graph cannot be mistaken for a complete one. The web UI shows a banner when the view is truncated.

### Added
- Microsoft Teams alerter with configurable webhook URL.

### Upgrade notes
- If your workflow consumed `aib.db` from the uploaded artifact, set `upload-database: true` to restore it.
- If an API client relied on `GET /api/v1/graph` returning every node, pass `?limit=0`.
- **Anyone who ran v1.4.5 or earlier against an Ansible inventory should treat previously uploaded workflow artifacts and existing `aib.db` files as containing plaintext credentials.** This release only protects future scans; it does not sanitize data already written.

## [1.1.0] - 2026-02-17

### Added
- **Security audit engine**: 12 built-in checks across three severity levels (critical, warning, info)
  - Detects public databases, unencrypted storage, permissive firewall rules, missing deletion protection, single-AZ databases, public S3 buckets, privileged containers, host namespace usage, public load balancers, orphan secrets, public instances, and container security best practices
  - CLI: `aib graph audit` with `--output json` support
  - API: `GET /api/v1/graph/analysis/audit`
- **Security metadata extraction** in parsers:
  - Terraform: `encrypted`, `storage_encrypted`, `publicly_accessible`, `deletion_protection`, `multi_az`, ingress/egress CIDRs, S3 versioning/logging
  - CloudFormation: `PubliclyAccessible`, `StorageEncrypted`, `DeletionProtection`, `MultiAZ`, security group ingress CIDRs, S3 `AccessControl`
  - Pulumi: `encrypted`, `storageEncrypted`, `publiclyAccessible`, `deletionProtection`, `multiAz`, ingress CIDRs
  - Kubernetes: container security context (`privileged`, `runAsNonRoot`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation`, `runAsUser`), pod-level `hostNetwork`/`hostPID`/`hostIPC`, `serviceAccountName`
- **Visual security risk indicators** in web UI:
  - Red borders on nodes with critical audit findings
  - Orange borders on nodes with warning audit findings
  - Security findings banner in node detail panel
- **Built-in AWS service icons**: embedded SVG icons for 17 AWS resource types (no CDN dependency)
- **JSON output**: global `--output json` / `-o json` flag for all 15 CLI query commands

## [1.0.0] - 2026-02-16

### Added
- **v1.0 stable release**: AIB is production-ready for single-instance infrastructure mapping
- OpenAPI spec updated to 1.0.0 with 401/429 error responses on all API endpoints
- Documented `/api/v1/openapi.json` and `/api/docs` routes in OpenAPI spec
- Rate limiting and authentication behavior documented in API spec description

### Improved
- Dockerfile updated to Go 1.25 + Alpine 3.21 (matches go.mod 1.25.7+ requirement)
- ROADMAP: marked CLI coverage improvement (#11) as completed (54% via cliApp DI refactor)

## [0.7.0] - 2026-02-09

### Added
- **Slack alerter**: dedicated Slack incoming webhook integration for certificate expiry notifications
  - Block Kit formatted messages with severity-colored sidebar (red/yellow/green)
  - Asset details (name, type, ID, expiry), message body, and optional blast radius info
  - Configurable channel override (`alerts.slack.channel`)
  - HTTPS-only webhook URL validation
  - Environment variable expansion for webhook URL (`${AIB_SLACK_WEBHOOK_URL}`)
  - Config: `alerts.slack.enabled`, `alerts.slack.webhook_url`, `alerts.slack.channel`

### Improved
- Extracted `buildAlerters()` helper in CLI to DRY up duplicated alerter wiring (cert check + serve commands)
- Updated Go version requirement from 1.22+ to 1.25.7+ in README

## [0.6.0] - 2026-02-09

### Added
- **Pulumi parser**: scan Pulumi stack export files for infrastructure resource dependencies (`aib scan pulumi`)
  - Supports ~80 resource types across AWS, GCP, Azure (native + classic), Kubernetes, and TLS providers
  - Edge creation from dependencies array, attribute references (vpcId, subnetId, securityGroupIds), and parent URNs
  - Metadata extraction: instanceType, machineType, vmSize, region, zone, tags, labels, ARN, and more
  - Two-phase parsing with cross-file edge resolution (same pattern as Terraform/CloudFormation)
  - Config file support: `sources.pulumi[].path`
  - API: `POST /api/v1/scan` now accepts `"pulumi"` as a source
  - Also added missing `"cloudformation"` to API scan trigger validation

## [0.5.0] - 2026-02-09

### Added
- **Drift detection**: scan-time diffing compares parser output against current store state before upserting
  - Detects added, removed, and modified nodes; added and removed edges
  - Source-scoped: a terraform scan won't flag kubernetes nodes as removed
  - Drift summary stored per scan in `scan_diffs` table (lightweight JSON)
  - CLI output shows drift after each scan (added/removed/modified counts)
  - API: `GET /api/v1/scans/{id}/diff` returns drift summary for a scan
  - OpenAPI spec updated with DriftSummary schema and new endpoint

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

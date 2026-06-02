# GitHub Action

AIB can run as a thin GitHub Action wrapper around the same CLI used locally. The action builds or downloads `aib`, scans infrastructure files, writes Markdown/JSON reports, uploads artifacts, and optionally updates a pull-request comment.

## Example

```yaml
name: AIB Infra Scan

on:
  pull_request:
    paths:
      - "**/*.tfstate"
      - "**/*tfplan*.json"
      - "**/*.yaml"
      - "**/*.yml"
      - "**/docker-compose*.yml"
      - "**/Pulumi*.json"

permissions:
  contents: read
  pull-requests: write

jobs:
  aib:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6

      - uses: matijazezelj/aib@v1.4.3
        with:
          paths: |
            .
          sources: auto
          comment-pr: true
          fail-on: critical
          upload-artifacts: true
```

Use the release tag that contains the Action. After a moving `v1` major tag is published, `matijazezelj/aib@v1` is also suitable for users who prefer automatic minor updates.

## Inputs

| Input | Default | Description |
|---|---|---|
| `paths` | `.` | Newline or comma separated paths to scan. Directories are walked by `sources: auto`. |
| `sources` | `auto` | `auto`, or comma-separated explicit scanners: `terraform`, `terraform-plan`, `kubernetes`, `compose`, `cloudformation`, `pulumi`, `ansible`. |
| `aib-version` | `source` | `source` builds the CLI from the action checkout. Set a release tag such as `v1.2.3` to download a release binary. |
| `comment-pr` | `true` | Create or update a PR comment using marker `<!-- aib-report -->`. Requires `pull-requests: write`. |
| `fail-on` | `critical` | Fail the job for findings at or above `critical`, `warning`, or `info`. Use `none` to never fail on findings. |
| `upload-artifacts` | `true` | Upload `aib.db`, `aib-report.md`, and `aib-report.json`. |
| `artifact-name` | `aib-report` | Artifact name. |
| `output-dir` | `.aib` | Directory for the SQLite DB and reports. |

## Outputs

| Output | Description |
|---|---|
| `findings-count` | Total security findings. |
| `critical-count` | Critical findings count. |
| `warning-count` | Warning findings count. |
| `info-count` | Info findings count. |
| `nodes-count` | Graph node count. |
| `edges-count` | Graph edge count. |
| `markdown-report-path` | Markdown report path. |
| `json-report-path` | JSON report path. |

## Auto detection

`scan auto` walks the supplied paths and groups supported files by scanner:

- Terraform state: `*.tfstate`
- Terraform plan JSON: filenames containing `tfplan` and ending in `.json`
- Docker Compose: `docker-compose*.yml` / `docker-compose*.yaml`
- CloudFormation: YAML/JSON under paths containing `cloudformation` or `/cfn/`
- Pulumi: JSON under paths containing `pulumi`
- Ansible: INI/YAML under paths containing `ansible`
- Kubernetes: other YAML manifests

Auto detection is intentionally conservative. If it guesses wrong for your repo, pass explicit `sources` and narrower `paths`.

## Security posture

The action is read-only by default:

- No cloud credentials are required.
- AIB parses files already present in the checked-out repository.
- PR comments contain summarized graph/audit data, not raw parsed file bodies.
- The SQLite DB and JSON report may contain resource names and metadata from your IaC; keep artifacts private for private infrastructure.

For public repositories, prefer `fail-on: critical` and review artifacts before making them broadly available. Secret redaction is a parser responsibility; don't put raw secret values in IaC and then act surprised when tools can see them. That's not a scanner problem, that's a "why is this in Git" problem.

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

      - uses: matijazezelj/aib@v1.5.0
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
| `upload-artifacts` | `true` | Upload `aib-report.md` and `aib-report.json`. On a public repository, workflow artifacts are downloadable by anyone. |
| `upload-database` | `false` | Also upload `aib.db` (as `<artifact-name>-db`). It contains the full asset graph including all extracted metadata, so it is off by default. |
| `artifact-name` | `aib-report` | Artifact name. |
| `output-dir` | `.aib` | Directory for the SQLite DB and reports. |
| `baseline-report` | empty | Optional previous AIB JSON report path. When set, Markdown/JSON reports include added/removed/changed assets and edges plus added/resolved findings. |

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
| `added-assets-count` | Assets added compared with `baseline-report`. |
| `removed-assets-count` | Assets removed compared with `baseline-report`. |
| `changed-assets-count` | Assets changed compared with `baseline-report`. |
| `added-findings-count` | Findings added compared with `baseline-report`. |
| `resolved-findings-count` | Findings resolved compared with `baseline-report`. |

## Baseline diffs

Pass a previous `aib-report.json` artifact as `baseline-report` to turn the
report into a PR-friendly delta instead of only a point-in-time inventory:

```yaml
- uses: actions/download-artifact@v6
  id: baseline
  with:
    name: aib-report-main
    path: .aib-baseline

- uses: matijazezelj/aib@v1.5.0
  with:
    paths: .
    sources: auto
    baseline-report: .aib-baseline/aib-report.json
```

If the baseline file is configured but missing, the action fails clearly rather
than silently pretending it compared something. Novel concept, apparently.

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
- The JSON report contains resource names and metadata from your IaC. On a public repository, workflow artifacts are downloadable by anyone — keep `upload-artifacts: false` if that inventory is sensitive.
- `aib.db` holds the full asset graph and is **not** uploaded unless you set `upload-database: true`.
- Release binaries are verified against the published `checksums.txt` before execution.

### Secret redaction

Parsers redact credentials before anything is written to the graph, by key name (`password`, `*_pass`, `*_token`, `secret`, `api_key`, …) and by value shape (URL DSNs, libpq keyword DSNs, secret-bearing query parameters). Host, port, database, username, and the key names survive so the graph still shows that a credential is configured.

This is defence in depth, not a licence to commit secrets. Redaction is deliberately biased toward over-matching, but it cannot recognise a credential stored under an innocuous key with no recognisable shape. Keep secrets out of IaC and inventory files.

Versions **≤ v1.4.5 did not redact at all**. If you ran those against Ansible inventories, treat previously uploaded artifacts and existing `aib.db` files as containing plaintext credentials — upgrading does not sanitize data already written.

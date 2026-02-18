# Configuration

AIB works with sensible defaults and no configuration file. To customize behavior, create `aib.yaml` in the working directory or in `~/.aib/`.

Also see [`configs/aib.yaml.example`](../configs/aib.yaml.example) for the full annotated reference.

## Quick Reference

The most common settings to override:

| Setting | Default | Description |
|---------|---------|-------------|
| `storage.path` | `./data/aib.db` | SQLite database location |
| `server.listen` | `:8080` | HTTP listen address |
| `server.api_token` | _(none)_ | Bearer token for API auth |
| `scan.allowed_paths` | _(none)_ | Restrict scan paths |
| `scan.schedule` | `4h` | Auto-scan interval |
| `certs.probe_interval` | `6h` | TLS probe interval |

## Full Example

```yaml
storage:
  path: "./data/aib.db"
  memgraph:
    enabled: false
    uri: "bolt://localhost:7687"
    username: ""
    password: ""

server:
  listen: ":8080"
  read_only: false
  api_token: "${AIB_API_TOKEN}"
  cors_origin: ""

scan:
  schedule: "4h"
  on_startup: true
  allowed_paths:
    - "/opt/infra/terraform"
    - "/opt/infra/k8s"

certs:
  probe_enabled: true
  probe_interval: "6h"
  alert_thresholds: [90, 60, 30, 14, 7, 1]

alerts:
  stdout:
    enabled: true
  webhook:
    enabled: false
    url: "http://sib:8080/api/v1/events"
    headers:
      Authorization: "Bearer ${AIB_WEBHOOK_TOKEN}"
  slack:
    enabled: false
    webhook_url: "https://hooks.slack.com/services/T.../B.../xxx"
    channel: ""
```

## Environment Variables

All settings support `${ENV_VAR}` expansion in YAML values. Settings can also be overridden with `AIB_`-prefixed environment variables using underscores for nesting:

| Config key | Environment variable |
|------------|---------------------|
| `storage.path` | `AIB_STORAGE_PATH` |
| `server.listen` | `AIB_SERVER_LISTEN` |
| `server.api_token` | `AIB_SERVER_API_TOKEN` |
| `scan.schedule` | `AIB_SCAN_SCHEDULE` |

## CLI Flags

Global flags available on all commands:

| Flag | Description |
|------|-------------|
| `--config` | Config file path (default: `./aib.yaml`) |
| `--db` | Database path (overrides `storage.path`) |
| `--log-format` | `text` or `json` (default: `text`) |
| `--log-level` | `debug`, `info`, `warn`, or `error` (default: `info`) |
| `-o, --output` | Output format: `text` or `json` (default: `text`) |

## Shell Completion

```bash
aib completion bash    # Bash
aib completion zsh     # Zsh
aib completion fish    # Fish
aib completion powershell
```

## Memgraph

SQLite is the source of truth. Optionally enable [Memgraph](https://github.com/memgraph/memgraph) for faster graph traversals (blast radius, shortest path, neighbor queries) at scale.

```yaml
storage:
  memgraph:
    enabled: true
    uri: "bolt://localhost:7687"
```

```bash
# Start Memgraph
docker run -p 7687:7687 memgraph/memgraph-mage

# Sync existing data
aib graph sync
```

When enabled, writes go to both SQLite and Memgraph through `SyncedStore`. You can rebuild Memgraph at any time with `aib graph sync`. The `deploy/docker-compose.yml` includes both services pre-configured.

For small environments (under ~10K assets), SQLite-only mode is sufficient.

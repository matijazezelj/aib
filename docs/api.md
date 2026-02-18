# API Reference

API docs are also available at `/api/docs` (Swagger UI) and `/api/v1/openapi.json` (OpenAPI 3.0 spec) when running `aib serve`.

## Endpoints

Most endpoints are read-only graph queries. The `POST /api/v1/scan` endpoint is the main mutating operation.

### Health & Metrics

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Health check |
| `GET` | `/metrics` | Prometheus metrics |

### Graph

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/graph` | Full graph (nodes + edges) |
| `GET` | `/api/v1/graph/nodes` | List nodes (`?type=`, `?source=`, `?provider=`) |
| `GET` | `/api/v1/graph/nodes/{id}` | Single node details |
| `GET` | `/api/v1/graph/edges` | List edges (`?type=`, `?from=`, `?to=`) |
| `GET` | `/api/v1/graph/shortest-path` | Shortest path (`?from=`, `?to=`) |
| `GET` | `/api/v1/graph/dependency-chain/{nodeId}` | Downstream dependencies (`?depth=`) |

### Analysis

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/impact/{nodeId}` | Blast radius |
| `GET` | `/api/v1/plan/impact` | Terraform plan impact analysis |
| `GET` | `/api/v1/graph/analysis/cycles` | Circular dependencies |
| `GET` | `/api/v1/graph/analysis/spof` | Single points of failure (`?min_affected=`, `?limit=`) |
| `GET` | `/api/v1/graph/analysis/orphans` | Orphan nodes |
| `GET` | `/api/v1/graph/analysis/audit` | Security audit findings |

### Certificates

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/certs` | All tracked certificates |
| `GET` | `/api/v1/certs/expiring` | Expiring certificates (`?days=30`) |

### Scans

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/scans` | Scan history |
| `GET` | `/api/v1/scans/{id}/diff` | Drift diff for a scan |
| `GET` | `/api/v1/scan/status` | Check if a scan is running |
| `POST` | `/api/v1/scan` | Trigger a scan (JSON body) |

### Export & Stats

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/stats` | Summary statistics |
| `GET` | `/api/v1/export/json` | Export graph as JSON |
| `GET` | `/api/v1/export/dot` | Export graph as Graphviz DOT |
| `GET` | `/api/v1/export/mermaid` | Export graph as Mermaid |
| `GET` | `/api/v1/openapi.json` | OpenAPI 3.0 spec |
| `GET` | `/api/docs` | Swagger UI |

## Triggering Scans

```bash
curl -X POST http://localhost:8080/api/v1/scan \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"source": "terraform", "paths": ["/opt/infra/terraform"]}'
```

Valid sources: `terraform`, `terraform-plan`, `kubernetes`, `kubernetes-live`, `ansible`, `compose`, `cloudformation`, `pulumi`, `all`.

## Authentication

Protect API endpoints with bearer token auth:

```yaml
server:
  api_token: "${AIB_API_TOKEN}"
```

Alternatively, set via environment variable: `AIB_SERVER_API_TOKEN=secret aib serve`.

Auth applies to `/api/*` routes only. The web UI, static assets, `/healthz`, and `/metrics` are always accessible without authentication.

## Security

AIB is intended for trusted internal networks. Built-in protections include:

- Strict Content Security Policy headers
- API rate limiting (10 requests/second per IP)
- Request body size limit (1 MB)
- Path traversal checks on scan paths
- Scan path allowlisting via `scan.allowed_paths`

Do not expose `aib serve` directly to the public internet. Place it behind a reverse proxy with TLS termination.

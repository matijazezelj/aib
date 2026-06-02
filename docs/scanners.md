# Scanners

AIB ships with seven parsers. Each accepts multiple paths, and cross-file references are resolved automatically. For CI and broad repository scans, `aib scan auto <path>` walks directories and groups supported files by scanner.

## Auto Detection

`scan auto` is a convenience command for pull requests and mixed IaC repositories. It detects Terraform state, Terraform plan JSON, Kubernetes YAML, Docker Compose, CloudFormation, Pulumi exports, and Ansible inventory/playbook files, then runs the underlying scanners against grouped paths.

```bash
aib scan auto .
aib scan auto infra/ deploy/docker-compose.yml
```

Auto detection is conservative. Use explicit scanner commands when the repository has ambiguous YAML or generated files.

## Terraform State

Parses `.tfstate` files with 100+ mapped resource types across AWS, GCP, Azure, Cloudflare, and TLS providers. Edges are derived from `dependencies` arrays and attribute references (`vpc_id`, `subnet_id`, `security_groups`, etc.).

**Security metadata extracted:** `encrypted`, `storage_encrypted`, `publicly_accessible`, `deletion_protection`, `multi_az`, security group ingress/egress CIDRs, S3 versioning and logging status.

**Node IDs:** `tf:<assetType>:<name>`

```bash
aib scan terraform terraform.tfstate
aib scan terraform /path/to/terraform/directory/
aib scan terraform networking.tfstate compute.tfstate   # cross-state edges

# Remote backends (requires terraform CLI)
aib scan terraform --remote project/
aib scan terraform --remote --workspace='*' project-a/ project-b/
```

## Terraform Plan

Parses `terraform show -json` output for pre-deploy impact analysis. Changes are classified as create, update, delete, or replace. Destructive actions can be scored for blast radius before any changes are applied.

Node IDs are compatible with state-scanned nodes, so plan results integrate cleanly with existing graph data.

```bash
terraform plan -out=tfplan && terraform show -json tfplan > plan.json
aib scan terraform-plan plan.json
aib scan terraform-plan infra-plan.json services-plan.json
```

## Kubernetes / Helm

Scans YAML manifests or Helm charts and discovers workloads (Deployments, StatefulSets, DaemonSets, Jobs, CronJobs), Services, Ingresses, Secrets, ConfigMaps, Certificates, and their relationships (label selectors, TLS termination, volume/secret mounts, `envFrom`, etc.).

**Security context metadata** is extracted per container: `privileged`, `runAsNonRoot`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation`, `runAsUser`. Pod-level flags: `hostNetwork`, `hostPID`, `hostIPC`, `serviceAccountName`.

The parser also infers `connects_to` edges from runtime configuration values (service hosts in environment variables and ConfigMaps), so app-to-service dependencies appear even without explicit IaC edges.

**Node IDs:** `k8s:<assetType>:<namespace>/<name>`

```bash
aib scan k8s deployment.yaml
aib scan k8s /path/to/manifests/
aib scan k8s /path/to/chart --helm --values=values-prod.yaml

# Live cluster scanning (requires kubectl)
aib scan k8s --live
aib scan k8s --live --kubeconfig=~/.kube/config --context=prod --namespace=app
```

### Live Cluster Scanning

With `--live`, AIB calls `kubectl` to fetch resources from a running cluster. It scans both namespaced resources (deployments, services, configmaps, secrets, ingresses) and cluster-scoped resources (clusterroles, clusterrolebindings). Namespaces matching `kube-system`, `kube-public`, and `kube-node-lease` are skipped by default.

## Ansible

Parses inventory files (INI and YAML formats) to discover hosts. With `--playbooks`, it also discovers containers and services from `docker_container` and `service` tasks in playbooks.

Inventory variables are used to infer dependency edges. Recognized variable keys include `db_host`, `database_host`, `postgres_host`, `mysql_host`, `redis_host`, `cache_host`, `k8s_service`, and their plural forms. When possible, inferred database nodes include `connection_string` metadata (auto-built from host/port/name variables, or taken from an explicit `db_connection_string` variable).

**Node IDs:** `ansible:<assetType>:<hostname>`

```bash
aib scan ansible inventory.ini
aib scan ansible inventory.yml
aib scan ansible staging.ini production.ini --playbooks=./playbooks/
```

## Docker Compose

Parses Docker Compose files into services, networks, and volumes. Dependency edges come from `depends_on`, network membership, and volume mounts.

Edges include connection evidence metadata (`via`, `raw_value`) so the reason for each relationship is traceable.

**Node IDs:** `compose:<assetType>:<name>`

```bash
aib scan compose docker-compose.yml
aib scan compose docker-compose.yml docker-compose.override.yml
```

## CloudFormation

Parses AWS CloudFormation templates (YAML and JSON) with ~40 mapped resource types. Edges are derived from `DependsOn`, `Ref`, `Fn::GetAtt`, and common property references (`VpcId`, `SubnetId`, `SecurityGroupIds`).

Each edge includes provenance metadata (`via`, `raw_value`). **Security metadata:** `PubliclyAccessible`, `StorageEncrypted`, `DeletionProtection`, `MultiAZ`, security group ingress CIDRs, S3 `AccessControl`.

**Node IDs:** `cfn:<assetType>:<logicalId>`

```bash
aib scan cloudformation template.yaml
aib scan cloudformation vpc.yaml compute.yaml database.json
```

## Pulumi

Parses `pulumi stack export` JSON with ~80 mapped resource types across AWS, GCP, Azure, Kubernetes, and TLS providers. Edges come from dependency arrays, attribute references, and parent URNs.

Attribute edges carry provenance metadata (`via`, `raw_value`). **Security metadata:** `encrypted`, `storageEncrypted`, `publiclyAccessible`, `deletionProtection`, `multiAz`, ingress CIDRs.

**Node IDs:** `plm:<assetType>:<name>`

```bash
aib scan pulumi stack-export.json
aib scan pulumi infra-stack.json app-stack.json
```

## External CLI Timeouts

Parsers that call external tools (`kubectl`, `helm`, `terraform`) apply a default command timeout when the caller does not provide a context deadline. This prevents scans from hanging indefinitely on unresponsive backends.

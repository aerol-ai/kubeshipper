# KubeShipper

A lightweight HTTP API service that manages Kubernetes workloads. Two APIs:

- **`/services`** — send a JSON spec, KubeShipper produces Deployment + Service + Ingress and applies via server-side apply.
- **`/charts`** — drive the Helm v3 SDK over HTTP: install / upgrade / uninstall / rollback / disable individual chart resources, with SSE progress streaming.

Single Go binary, single SQLite file for local state, no sidecars.

## Table of Contents

1. [API Reference](#api-reference)
2. [Running Locally](#running-locally)
3. [Environment Variables](#environment-variables)
4. [Deploying via Helm (recommended)](#deploying-via-helm-recommended)
5. [Deploying via Raw Manifests](#deploying-via-raw-manifests)
6. [Required Kubernetes Permissions](#required-kubernetes-permissions)
7. [Namespace-scoped Access](#namespace-scoped-access)

---

## API Reference

### `/services` — JSON-spec deployments

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/services` | Deploy a new service — returns `jobId` + SSE stream URL |
| `GET` | `/services` | List all services |
| `GET` | `/services/:id` | Get a service + live K8s status |
| `PATCH` | `/services/:id` | Update a service spec — returns `jobId` + SSE stream URL |
| `DELETE` | `/services/:id` | Tear down a service — returns `jobId` + SSE stream URL |
| `POST` | `/services/:id/restart` | Rolling restart — returns `jobId` + SSE stream URL |
| `GET` | `/services/:id/logs` | Stream live pod logs |
| `GET` | `/services/jobs/:jobId` | Job state + accumulated events |
| `GET` | `/services/jobs/:jobId/stream` | Server-Sent Events for a deploy/patch/delete/restart job |

Every mutating call on `/services` is fire-and-stream: a 202 response with a
`jobId` and the SSE URL to consume progress from. There's no opt-in flag —
streaming is the only path.

### `/charts` — Helm chart management

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/charts` | Install a chart (returns 202 + jobId + SSE URL) |
| `GET` | `/charts` | Live list from Helm |
| `POST` | `/charts/preflight` | Run checks without installing |
| `GET` | `/charts/:release?namespace=` | Release detail + values + manifest + disabled list |
| `PATCH` | `/charts/:release?namespace=` | Upgrade (auto drift-resync) |
| `DELETE` | `/charts/:release?namespace=&force=true` | Uninstall + reap PVCs |
| `POST` | `/charts/:release/rollback?namespace=` | Roll back to revision |
| `GET` | `/charts/:release/history\|values\|manifest?namespace=` | Read paths |
| `POST` | `/charts/:release/resources/:kind/:name/disable?namespace=&force=true` | Strip a single resource via post-renderer |
| `POST` | `/charts/:release/resources/:kind/:name/enable?namespace=` | Re-add a stripped resource |
| `GET` | `/charts/jobs/:jobId\|/stream` | Job state + SSE event stream |

`/charts` supports four chart sources: OCI registries (incl. private GHCR), classic HTTPS Helm repos, git URLs, and uploaded `.tgz`. Credentials are supplied per-request and never persisted. See `docs/charts-api.md` for full payload examples.

### Always-public

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Liveness/readiness check |
| `GET` | `/` | Service info |

### Example request body

```json
{
  "name": "my-api",
  "image": "ghcr.io/my-project/my-api:abc1234",
  "port": 3000,
  "env": { "NODE_ENV": "production" },
  "replicas": 2,
  "public": true,
  "hostname": "my-api.example.com",
  "imagePullSecret": "gcr-pull-secret",
  "resources": {
    "requests": { "cpu": "100m", "memory": "128Mi" },
    "limits":   { "cpu": "500m", "memory": "256Mi" }
  }
}
```

### Authentication

When `AUTH_TOKEN` is set, all `/services` and `/charts` endpoints require:

```
Authorization: Bearer <your-token>
```

`/health` and `/` are always public (used by K8s probes).

---

## Running Locally

Requirements: **Go 1.22+** and a kubeconfig.

```bash
# 1. Resolve dependencies (one-time)
go mod tidy

# 2. Copy and edit the env file
cp .env.example .env

# 3. Start the server (uses your current kubectl context)
MANAGED_NAMESPACES=default go run .
```

The server starts on `http://localhost:3000`. Your local `~/.kube/config` is used automatically when running outside a cluster.

Quick smoke test:

```bash
curl http://localhost:3000/health
curl http://localhost:3000/charts        # lists Helm releases in your current cluster
```

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3000` | HTTP port the server listens on |
| `AUTH_TOKEN` | _(unset)_ | Bearer token for API auth. Leave unset to disable auth (dev mode). |
| `DB_PATH` | `kubeshipper.sqlite` | Path to the SQLite database file. Point at a PVC mount in Kubernetes. |
| `MANAGED_NAMESPACES` | _(required)_ | Comma-separated allow-list of namespaces `/services` may deploy into. The server refuses to start if unset. Example: `default,production,staging`. |
| `APP_VERSION` | `dev` | Returned by `/health` for diagnostics. Usually injected by CI from the git SHA. |
| `KUBECONFIG` | _(unset)_ | Path to a kubeconfig file. Falls back to in-cluster service account when unset. |

---

## Deploying via Helm (recommended)

### Prerequisites

- Helm 3.x installed
- `kubectl` configured and pointing at your cluster
- Docker image pushed to your registry

### Install

```bash
helm install kubeshipper ./helm-chart \
  --namespace kubeshipper \
  --create-namespace \
  --set image.repository=ghcr.io/aerol-ai/kubeshipper \
  --set image.tag=latest \
  --set auth.token=your-secret-token
```

### Upgrade

```bash
helm upgrade kubeshipper ./helm-chart \
  --namespace kubeshipper \
  --set image.tag=abc1234
```

### Uninstall

```bash
helm uninstall kubeshipper --namespace kubeshipper
```

### Key values

| Value | Default | Description |
|-------|---------|-------------|
| `image.repository` | `ghcr.io/aerol-ai/kubeshipper` | Container image repository |
| `image.tag` | `""` (chart appVersion) | Image tag |
| `auth.token` | `""` | Bearer token. Empty = no auth. |
| `auth.existingSecret` | `""` | Use a pre-existing K8s Secret instead of creating one. Must have key `auth-token`. |
| `storage.size` | `1Gi` | PVC size for SQLite |
| `storage.storageClass` | `""` | StorageClass name. Empty = cluster default. |
| `rbac.clusterWide` | `true` | Controls `/services` RBAC. `true` = ClusterRole (any namespace). `false` = Role per namespace. |
| `rbac.managedNamespaces` | `["default"]` | Namespaces `/services` may deploy into. Drives the `MANAGED_NAMESPACES` env. |
| `rbac.helmAdmin` | `false` | Required for `/charts`. Binds the SA to `cluster-admin` so Helm can install charts containing CRDs / cluster-scoped resources. |
| `replicaCount` | `1` | **Do not increase.** SQLite requires a single writer. |
| `service.type` | `ClusterIP` | K8s Service type for KubeShipper's own API |

#### Enabling `/charts`

`/charts` lets the API install Helm charts (including charts that contain `ClusterIssuer`, CRDs, multi-namespace resources). Those need cluster-scoped privileges that the default RBAC does not grant. Set `rbac.helmAdmin=true` to bind a `ClusterRoleBinding` to `cluster-admin`:

```bash
helm install kubeshipper ./helm-chart \
  --namespace kubeshipper --create-namespace \
  --set auth.token=your-secret-token \
  --set rbac.helmAdmin=true
```

> ⚠️ Setting `helmAdmin=true` makes `AUTH_TOKEN` cluster-admin-equivalent — the holder can install any Helm chart, which can create any Kubernetes resource. Keep the token tightly held.

### Namespace-scoped Helm install

To restrict KubeShipper to only manage the `production` and `staging` namespaces:

```bash
helm install kubeshipper ./helm-chart \
  --namespace kubeshipper \
  --create-namespace \
  --set image.repository=ghcr.io/aerol-ai/kubeshipper \
  --set auth.token=your-secret-token \
  --set managedNamespace=production \
  --set rbac.clusterWide=false \
  --set rbac.managedNamespaces[0]=production \
  --set rbac.managedNamespaces[1]=staging
```

Or via a custom values file (`my-values.yaml`):

```yaml
image:
  repository: ghcr.io/aerol-ai/kubeshipper
  tag: latest

auth:
  token: your-secret-token

managedNamespace: production

rbac:
  clusterWide: false
  managedNamespaces:
    - production
    - staging
```

```bash
helm install kubeshipper ./helm-chart -f my-values.yaml --namespace kubeshipper --create-namespace
```

---

## Deploying via Raw Manifests

> These manifests are in `k8s/`. Apply them in order.

### Step 1 — Create the namespace (optional)

```bash
kubectl create namespace kubeshipper
```

### Step 2 — Apply RBAC

**Option A — Cluster-wide access (simplest, default)**

KubeShipper can deploy workloads into any namespace:

```bash
kubectl apply -f k8s/rbac.yaml
```

**Option B — Namespace-scoped access (recommended for production)**

KubeShipper can only manage workloads in the namespaces you specify:

```bash
# Edit k8s/rbac-namespaced.yaml to list your target namespaces, then:
kubectl apply -f k8s/rbac-namespaced.yaml
```

### Step 3 — Create the auth secret (optional)

```bash
kubectl create secret generic kubeshipper-secrets \
  --namespace default \
  --from-literal=auth-token=your-secret-token
```

If the secret doesn't exist, the API runs without authentication.

### Step 4 — Deploy KubeShipper

```bash
# Edit k8s/deployment.yaml: set image, MANAGED_NAMESPACES, etc.
kubectl apply -f k8s/deployment.yaml
```

### Step 5 — Verify

```bash
kubectl get pods -l app=kubeshipper
kubectl logs -l app=kubeshipper -f
curl http://<POD_IP>:3000/health
```

### Accessing the API from outside the cluster

KubeShipper's Service is `ClusterIP` by default. To expose it:

```bash
# Port-forward for temporary access
kubectl port-forward svc/kubeshipper 3000:3000

# Or change the Service type to LoadBalancer in deployment.yaml
```

---

## Required Kubernetes Permissions

`/services` needs only the narrow, namespace-scoped permissions below. `/charts` needs `cluster-admin` because Helm charts can include CRDs, cluster-scoped resources, and resources in multiple namespaces.

### `/services` (default RBAC)

| API Group | Resources | Verbs | Why |
|-----------|-----------|-------|-----|
| `apps` | `deployments`, `deployments/status` | get, list, watch, create, update, patch, delete | Create and manage application Deployments; read rollout status |
| _(core)_ | `pods` | get, list, watch | Find pods to stream logs from; poll readiness |
| _(core)_ | `pods/log` | get | Stream live pod logs via `GET /services/:id/logs` |
| _(core)_ | `services` | get, list, watch, create, update, patch, delete | Create ClusterIP Services for internal networking |
| _(core)_ | `configmaps` | get, list, watch, create, update, patch, delete | Store non-sensitive environment configuration |
| _(core)_ | `secrets` | get, list, watch, create, update, patch, delete | Store sensitive credentials; manage image pull secrets |
| `networking.k8s.io` | `ingresses` | get, list, watch, create, update, patch, delete | Expose services publicly via `"public": true` |
| `batch` | `jobs`, `cronjobs` | get, list, watch, create, update, patch, delete | One-off Jobs (`"type": "job"`) and scheduled CronJobs (`"type": "cronjob"`) |

For `/services` only, KubeShipper does **not** need access to Nodes, PersistentVolumes, ClusterRoles, or any cluster-level resources. Blast radius is limited to the namespaces in `rbac.managedNamespaces`.

### `/charts` (set `rbac.helmAdmin=true`)

`/charts` binds the service account to the built-in `cluster-admin` ClusterRole. This is required because any chart you install might create CRDs, ClusterIssuers, namespaces, ClusterRoles, or resources outside `rbac.managedNamespaces`. There is no narrower role that reliably covers arbitrary Helm charts. If you only need `/services`, leave `helmAdmin` off.

---

## Namespace-scoped Access

By default, KubeShipper uses a `ClusterRole` + `ClusterRoleBinding`, which lets it deploy into **any namespace**. For production multi-tenant clusters this is often too broad.

### How namespace-scoped RBAC works

Instead of a ClusterRole (cluster-wide), you create a `Role` + `RoleBinding` **inside each namespace** you want KubeShipper to manage:

```
ClusterRole  + ClusterRoleBinding → deploy to ANY namespace
Role         + RoleBinding        → deploy only to THAT namespace
```

A RoleBinding can reference a ServiceAccount from a different namespace (kubeshipper's own namespace), so you don't need to run kubeshipper inside each managed namespace.

### Configuration

**1. Set the env var (in `.env` or `k8s/deployment.yaml`)**

```bash
MANAGED_NAMESPACES=production,staging
```

**2. Apply namespace-scoped RBAC**

Edit `k8s/rbac-namespaced.yaml` to list your namespaces, then:

```bash
kubectl apply -f k8s/rbac-namespaced.yaml
```

To add more namespaces later, copy the `Role` + `RoleBinding` block, change the `namespace` field, and re-apply.

**3. Verify**

```bash
# Confirm the RoleBinding exists in the target namespace
kubectl get rolebinding kubeshipper -n production

# Test that kubeshipper can list deployments in production
kubectl auth can-i list deployments \
  --namespace production \
  --as system:serviceaccount:default:kubeshipper
# → yes
```

---

## GitHub Actions + OCI packaging (GHCR)

The workflow file `.github/workflows/build-push-gcr.yml` builds and pushes on every push to `main` and on `v*` tags:

- **Container image** → `ghcr.io/aerol-ai/kubeshipper`
- **Helm OCI chart** → `oci://ghcr.io/aerol-ai/helm/kubeshipper`

Authentication uses the built-in `GITHUB_TOKEN` — no GCP account, no service account keys, no extra secrets required.

### Publish triggers

| Event | What is published |
|-------|-------------------|
| Push to `main` | image tagged `main` + short SHA; chart at current `version` in `Chart.yaml` |
| Push tag `v1.2.3` | image tagged `1.2.3` + `latest`; chart `appVersion` bumped to match |

### Install from GHCR OCI registry

`MANAGED_NAMESPACES` is **required** — the server will refuse to start without it. Pass it via `--set` or a values file.

**Minimal install (cluster-wide access, single namespace):**

```bash
helm install kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --version 0.1.0 \
  --namespace kubeshipper \
  --create-namespace \
  --set auth.token=your-secret-token \
  --set rbac.managedNamespaces[0]=default
```

**Namespace-scoped install (production + staging only):**

```bash
helm install kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --version 0.1.0 \
  --namespace kubeshipper \
  --create-namespace \
  --set auth.token=your-secret-token \
  --set rbac.clusterWide=false \
  --set rbac.managedNamespaces[0]=production \
  --set rbac.managedNamespaces[1]=staging
```

Or via a values file:

```yaml
# my-values.yaml
auth:
  token: your-secret-token

rbac:
  clusterWide: false
  managedNamespaces:
    - production
    - staging
```

```bash
helm install kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --version 0.1.0 \
  --namespace kubeshipper \
  --create-namespace \
  -f my-values.yaml
```

### Upgrade

```bash
helm upgrade kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --version 0.2.0 \
  --namespace kubeshipper \
  -f my-values.yaml
```
```

### Restrict via Helm

```bash
helm install kubeshipper ./helm-chart \
  --set rbac.clusterWide=false \
  --set rbac.managedNamespaces[0]=production \
  --set rbac.managedNamespaces[1]=staging
```

`/services` requests pick the target namespace from the `namespace` field on each request body, validated against the `MANAGED_NAMESPACES` allow-list. A request for an unlisted namespace is rejected with 400.

---

## Source Layout

```
main.go                     entry — boots HTTP, worker, SQLite
internal/
├── api/                    chi router + handlers
│   ├── server.go           /, /health, auth gate
│   ├── auth.go             bearer-token middleware
│   ├── services.go         /services/* (8 endpoints)
│   └── charts.go           /charts/* (15 endpoints, SSE)
├── helm/                   wraps the Helm v3 SDK directly (no sidecar)
│   ├── manager.go, install.go, upgrade.go, uninstall.go,
│   ├── rollback.go, list_get.go, preflight.go, diff.go,
│   ├── postrender.go       per-resource disable via Helm PostRenderer
│   ├── prereqs.go          provisions K8s Secrets the chart depends on
│   └── source/             oci.go, https.go, git.go, tgz.go
├── kube/                   client-go SSA + namespace allow-list
├── store/                  modernc.org/sqlite (pure Go, no CGO)
│   ├── services.go, jobs.go, disabled.go, audit.go
└── worker/                 PENDING → DEPLOYING → READY + drift
```

The Helm SDK is invoked in-process — there is no `helmd` sidecar, no gRPC, no proto file. Compiles to a single static binary (~54 MB) on alpine.

## Building the Docker Image

```bash
# Local build
docker build -t kubeshipper:local .

# Run locally (uses ~/.kube/config — for testing only)
docker run --rm \
  -e AUTH_TOKEN=dev \
  -e MANAGED_NAMESPACES=default \
  -v ~/.kube:/home/ks/.kube:ro \
  -p 3000:3000 \
  kubeshipper:local
```

## CI/CD — Pushing to GCR

The GitHub Actions workflow at `.github/workflows/build-push-gcr.yml` builds and pushes to `ghcr.io` on every push to `main` and on version tags (`v*`).

### Required GitHub secrets

| Secret | Description |
|--------|-------------|
| `GCP_PROJECT_ID` | Your GCP project ID |
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | Full WIF provider resource name |
| `GCP_SERVICE_ACCOUNT` | Service account email used for pushing to GCR |

The GCP service account needs the `roles/storage.admin` role (for GCR) or `roles/artifactregistry.writer` (for Artifact Registry).

### Alternative: Service Account Key

If you prefer a service account JSON key over Workload Identity Federation, replace the auth step in the workflow with:

```yaml
- uses: google-github-actions/auth@v2
  with:
    credentials_json: ${{ secrets.GCP_SA_KEY }}
```


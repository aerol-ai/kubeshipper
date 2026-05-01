# KubeShipper

A lightweight deployment control plane for Kubernetes workloads.

- **`/`** — React dashboard for Helm releases, service CRUD, rollout automation, and live operation streaming.
- **`/api/services`** — send a JSON spec, KubeShipper produces Deployment + Service + Ingress and applies via server-side apply.
- **`/api/charts`** — drive the Helm v3 SDK over HTTP: install / upgrade / uninstall / rollback / disable individual chart resources, with SSE progress streaming.
- **`/api/rollout-watches`** — register existing Deployments for automatic image-digest checks every minute and patch them when the remote digest changes.

Single Go binary, single SQLite file for local state, no sidecars. The backend resource surface is `/api/*`, while `/` is reserved for the dashboard.

## Table of Contents

1. [API Reference](#api-reference)
2. [Running Locally](#running-locally)
3. [Environment Variables](#environment-variables)
4. [Deploying via Helm (recommended)](#deploying-via-helm-recommended)
5. [Exposing KubeShipper externally (Ingress)](#exposing-kubeshipper-externally-ingress)
6. [Deploying via Raw Manifests](#deploying-via-raw-manifests)
7. [Required Kubernetes Permissions](#required-kubernetes-permissions)
8. [Namespace-scoped Access](#namespace-scoped-access)

---

## API Reference

All resource routes below are served under `/api/*`.

### `/api/services` — JSON-spec deployments

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/services` | Deploy a new service — returns `jobId` + SSE stream URL |
| `GET` | `/api/services` | List all services |
| `GET` | `/api/services/:id` | Get a service + live K8s status |
| `PATCH` | `/api/services/:id` | Update a service spec — returns `jobId` + SSE stream URL |
| `DELETE` | `/api/services/:id` | Tear down a service — returns `jobId` + SSE stream URL |
| `POST` | `/api/services/:id/restart` | Rolling restart — returns `jobId` + SSE stream URL |
| `GET` | `/api/services/:id/logs` | Stream live pod logs |
| `GET` | `/api/services/jobs/:jobId` | Job state + accumulated events |
| `GET` | `/api/services/jobs/:jobId/stream` | Server-Sent Events for a deploy/patch/delete/restart job |

Every mutating call on `/api/services` is fire-and-stream: a 202 response with a
`jobId` and the SSE URL to consume progress from. There's no opt-in flag —
streaming is the only path.

### `/api/charts` — Helm chart management

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/charts` | Install a chart (returns 202 + jobId + SSE URL) |
| `GET` | `/api/charts` | Live list from Helm |
| `POST` | `/api/charts/preflight` | Run checks without installing |
| `GET` | `/api/charts/:release?namespace=` | Release detail + values + manifest + disabled list |
| `PATCH` | `/api/charts/:release?namespace=` | Upgrade (auto drift-resync) |
| `DELETE` | `/api/charts/:release?namespace=&force=true` | Uninstall + reap PVCs |
| `POST` | `/api/charts/:release/rollback?namespace=` | Roll back to revision |
| `GET` | `/api/charts/:release/history\|diff\|values\|manifest?namespace=` | Read paths |
| `POST` | `/api/charts/:release/resources/:kind/:name/disable?namespace=&force=true` | Strip a single resource via post-renderer |
| `POST` | `/api/charts/:release/resources/:kind/:name/enable?namespace=` | Re-add a stripped resource |
| `GET` | `/api/charts/jobs/:jobId\|/stream` | Job state + SSE event stream |

`/api/charts` supports four chart sources: OCI registries (incl. private GHCR), classic HTTPS Helm repos, git URLs, and uploaded `.tgz`. Credentials are supplied per-request and never persisted. See `docs/charts-api.md` for full payload examples.

When a chart install or upgrade should also configure automatic digest-based restarts, include an optional `rolloutWatch` block in the same request body:

```json
{
  "release": "auto-coder",
  "namespace": "auto-coder",
  "source": {
    "type": "oci",
    "url": "oci://ghcr.io/acme/auto-coder",
    "version": "1.2.3"
  },
  "rolloutWatch": {
    "deployment": "agent-gateway"
  }
}
```

`rolloutWatch.service` is accepted as an alias for `rolloutWatch.deployment`, and `rolloutWatch.container` lets you target one container in a multi-container Deployment.

### `/api/rollout-watches` — automatic digest-based deployment refresh

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/rollout-watches` | Register or refresh a watched Deployment |
| `GET` | `/api/rollout-watches` | List watched Deployments + latest sync state |
| `GET` | `/api/rollout-watches/:id` | Read one watch, including its timeline |
| `POST` | `/api/rollout-watches/:id/enable` | Re-enable automatic digest reconciliation |
| `POST` | `/api/rollout-watches/:id/disable` | Pause automatic digest reconciliation |
| `POST` | `/api/rollout-watches/:id/sync` | Trigger an immediate digest check |
| `POST` | `/api/rollout-watches/:id/restart` | Force a rollout restart immediately |
| `DELETE` | `/api/rollout-watches/:id` | Remove a watch |

Behavior:

- The worker checks every registered Deployment once per minute inside the KubeShipper pod.
- The watch tracks the Deployment image reference, resolves the latest remote digest, and compares it to the currently running digest.
- Mutable tags like `:latest` are handled by digest, not tag text. When a new digest is published, KubeShipper patches the Deployment image to `image:tag@sha256:...` so Kubernetes performs a rollout.
- Registry auth is resolved from the Deployment's `imagePullSecrets` and its ServiceAccount `imagePullSecrets`, so private registries work without storing registry credentials in KubeShipper.

Example registration:

```json
{
  "namespace": "auto-coder",
  "deployment": "agent-gateway"
}
```

`service` is accepted as an alias for `deployment` to support callers that think in service names rather than Deployment names.

### Always-public

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | React dashboard shell |
| `GET` | `/health` | Liveness/readiness check |
| `GET` | `/api/` | JSON API docs |
| `GET` | `/api/health` | JSON health endpoint |

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

When `AUTH_TOKEN` is set, all `/api/*` resource endpoints require either:

```
Authorization: Bearer <your-token>
```

or a JWT session cookie minted by the dashboard login flow:

```text
POST /api/auth/login
{ "token": "<AUTH_TOKEN>" }
```

That endpoint sets an `HttpOnly` cookie used automatically by the dashboard for all subsequent `/api/*` requests. Session inspection and logout are available at `/api/auth/session` and `/api/auth/logout`.

`/health`, `/`, `/api/`, and `/api/health` are always public.

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

If you change the dashboard source in `web/`, rebuild the embedded assets with:

```bash
cd web && bun install && bun run build
```

Quick smoke test:

```bash
curl http://localhost:3000/health
curl http://localhost:3000/api/          # API docs JSON
curl http://localhost:3000/api/charts    # lists Helm releases in your current cluster
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

### Accessing the auth token

When you install with `--set auth.token=...`, the chart stores the token in a Kubernetes Secret named **`<release>-auth`** (e.g. `kubeshipper-auth`) in the release namespace, under the key **`auth-token`**. The pod reads it via `secretKeyRef`, so the Secret is the source of truth.

If you used `--set auth.token=$(openssl rand -hex 32)` at install time, the token was generated by the shell and you never saw it on screen. Retrieve it with:

```bash
kubectl get secret kubeshipper-auth -n kubeshipper \
  -o jsonpath='{.data.auth-token}' | base64 -d
echo
```

Or load it straight into a shell variable for use with `curl`:

```bash
TOKEN=$(kubectl get secret kubeshipper-auth -n kubeshipper \
  -o jsonpath='{.data.auth-token}' | base64 -d)

curl -H "Authorization: Bearer $TOKEN" https://shipper.example.com/health
```

If you don't know the exact secret name, list the secrets — the one you want ends in `-auth`:

```bash
kubectl get secrets -n kubeshipper
```

The naming pattern is `<helm release name>-auth`. If you used `auth.existingSecret`, the chart didn't create a secret — read from the one you supplied (key `auth-token`).

### Rotating the auth token

```bash
NEW=$(openssl rand -hex 32)
helm upgrade kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --namespace kubeshipper \
  --reuse-values \
  --set auth.token="$NEW"

# Pick up the new env from the Secret
kubectl rollout restart deploy/kubeshipper -n kubeshipper
```

The pod must restart to re-read the Secret — `secretKeyRef` env vars are not hot-reloaded.

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
| `ingress.enabled` | `false` | Render an external-access resource. See [Exposing externally](#exposing-kubeshipper-externally-ingress). |
| `ingress.provider` | `""` | Which ingress controller to target. Currently supported: `traefik`. |
| `ingress.host` | `""` | Public hostname (e.g. `shipper.example.com`). Required when enabled. |
| `ingress.tls.enabled` | `true` | Whether to serve TLS. Either `tls.secretName` or a provider-specific cert source must be set. |
| `ingress.tls.secretName` | `""` | Bring-your-own TLS secret containing `tls.crt`/`tls.key` for `host`. |
| `ingress.allowUnauthenticated` | `false` | Override the safety rail that refuses to expose KubeShipper without `auth.token` set. |
| `ingress.traefik.kind` | `IngressRoute` | `IngressRoute` (Traefik CRD) or `Ingress` (standard k8s, picked up by Traefik). |
| `ingress.traefik.entryPoints` | `[websecure]` | Traefik entrypoints (IngressRoute only). |
| `ingress.traefik.certResolver` | `""` | Traefik cert resolver (e.g. `letsencrypt`) for ACME. IngressRoute only. |
| `ingress.traefik.middlewares` | `[]` | References to existing Traefik Middleware CRDs (`{name, namespace}`). |

#### Enabling `/charts`

`/charts` lets the API install Helm charts (including charts that contain `ClusterIssuer`, CRDs, multi-namespace resources). Those need cluster-scoped privileges that the default RBAC does not grant. Set `rbac.helmAdmin=true` to bind a `ClusterRoleBinding` to `cluster-admin`:

```bash
helm install kubeshipper ./helm-chart \
  --namespace kubeshipper --create-namespace \
  --set auth.token=your-secret-token \
  --set rbac.helmAdmin=true
```

> ⚠️ Setting `helmAdmin=true` makes `AUTH_TOKEN` cluster-admin-equivalent — the holder can install any Helm chart, which can create any Kubernetes resource. Keep the token tightly held.

### Cluster-wide install (deploy to all namespaces)

To let KubeShipper deploy `/services` workloads into **any** namespace, leave
`rbac.clusterWide=true` (the default). This creates a `ClusterRole` +
`ClusterRoleBinding`, so you don't need to enumerate namespaces in
`rbac.managedNamespaces`:

```bash
helm install kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --version 0.1.2 \
  --namespace kubeshipper --create-namespace \
  --set auth.token=$(openssl rand -hex 32) \
  --set rbac.helmAdmin=true \
  --set rbac.clusterWide=true
```

There is no wildcard for the namespace-scoped mode — to grant access to a
fixed set of namespaces only, see the next section.

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

## Exposing KubeShipper externally (Ingress)

By default the chart only renders a `ClusterIP` Service — KubeShipper is reachable only from inside the cluster (or via `kubectl port-forward`). To expose it to clients outside the cluster, enable the `ingress` block.

The chart is **provider-pluggable**. Today only `traefik` is implemented; `nginx` and `caddy` are planned. The chart does not check whether your ingress controller is actually running — by setting `provider`, you assert it exists.

### Prerequisites

1. An ingress controller already running in the cluster (Traefik for the example below).
2. A DNS record pointing `host` at the controller's external IP/hostname. Set this **before** install if you use ACME — Let's Encrypt's HTTP-01 challenge will fail otherwise.
3. `auth.token` (or `auth.existingSecret`) set. The chart refuses to render an ingress without authentication unless you opt in to `ingress.allowUnauthenticated=true`.

### Traefik — IngressRoute (recommended)

```bash
helm install kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --version 0.1.2 \
  --namespace kubeshipper --create-namespace \
  --set auth.token=$(openssl rand -hex 32) \
  --set rbac.helmAdmin=true \
  --set rbac.clusterWide=true \
  --set ingress.enabled=true \
  --set ingress.provider=traefik \
  --set ingress.host=shipper.example.com \
  --set ingress.traefik.kind=IngressRoute \
  --set ingress.traefik.certResolver=letsencrypt
```

`certResolver` must match a resolver name from your Traefik static config. If you'd rather provide your own cert, drop the `certResolver` flag and pass `--set ingress.tls.secretName=my-existing-tls-secret`.

### Traefik — standard Ingress (portable)

Useful when you want a plain `networking.k8s.io/v1` Ingress that other controllers could also consume:

```bash
helm install kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --version 0.1.2 \
  --namespace kubeshipper --create-namespace \
  --set auth.token=$(openssl rand -hex 32) \
  --set ingress.enabled=true \
  --set ingress.provider=traefik \
  --set ingress.host=shipper.example.com \
  --set ingress.traefik.kind=Ingress \
  --set ingress.traefik.ingressClassName=traefik \
  --set ingress.tls.secretName=kubeshipper-tls
```

### Adding rate-limit / IP-allowlist via Traefik Middleware

Create the Middleware CRDs separately, then reference them from the chart:

```yaml
# my-values.yaml
ingress:
  enabled: true
  provider: traefik
  host: shipper.example.com
  traefik:
    kind: IngressRoute
    certResolver: letsencrypt
    middlewares:
      - name: shipper-ratelimit
        namespace: kubeshipper
      - name: shipper-ip-allowlist
        namespace: kubeshipper
```

The chart does not create middlewares — managing them stays your responsibility, which keeps this chart small and predictable.

### Validation

The chart fails at `helm install` time on any of the following — by design:

| Misconfiguration | Error |
|---|---|
| `ingress.enabled=true`, `provider=""` | `ingress.provider must be set when ingress.enabled is true` |
| Unsupported provider (e.g. `nginx`) | `ingress.provider "nginx" is not supported yet` |
| Missing `host` | `ingress.host is required when ingress.enabled is true` |
| TLS on, no `secretName` and no `certResolver` | `no cert source configured` |
| No `auth.token`, no `auth.existingSecret`, `allowUnauthenticated` not set | `refusing to expose kubeshipper without authentication` |

### Smoke test

Once the IngressRoute / Ingress is applied and DNS resolves:

```bash
curl -H "Authorization: Bearer $TOKEN" https://shipper.example.com/health
# {"started_at":"...","status":"ok","version":"..."}
```

> ⚠️ The bearer token is cluster-admin-equivalent when `rbac.helmAdmin=true`. Always pair external exposure with TLS, a strong random token, and ideally an IP-allowlist Middleware.

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

KubeShipper's Service is `ClusterIP` by default. For temporary access:

```bash
kubectl port-forward svc/kubeshipper 3000:3000
```

For a real external endpoint (TLS, hostname, ingress controller integration), use the Helm install path with the `ingress` block — see [Exposing KubeShipper externally](#exposing-kubeshipper-externally-ingress). The raw manifests in `k8s/` do not include an Ingress; you'd need to author one yourself.

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
  --version 0.1.2 \
  --namespace kubeshipper \
  --create-namespace \
  --set auth.token=your-secret-token \
  --set rbac.managedNamespaces[0]=default
```

**Namespace-scoped install (production + staging only):**

```bash
helm install kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --version 0.1.2 \
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
  --version 0.1.2 \
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

`/api/services` requests pick the target namespace from the `namespace` field on each request body, validated against the `MANAGED_NAMESPACES` allow-list. A request for an unlisted namespace is rejected with 400.

---

## Source Layout

```
main.go                     entry — boots HTTP, worker, SQLite
internal/
├── api/                    chi router + handlers
│   ├── server.go           /, /health, auth gate
│   ├── auth.go             bearer-token middleware
│   ├── services.go         /api/services/* (8 endpoints)
│   └── charts.go           /api/charts/* (15 endpoints, SSE)
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


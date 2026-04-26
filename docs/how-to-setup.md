# How to set up KubeShipper

End-to-end guide. Covers local development, Docker, and Kubernetes deployment.

- [1. Prerequisites](#1-prerequisites)
- [2. Local development](#2-local-development)
- [3. Build the Docker image](#3-build-the-docker-image)
- [4. Deploy to Kubernetes (Helm — recommended)](#4-deploy-to-kubernetes-helm--recommended)
- [5. Deploy to Kubernetes (raw manifests)](#5-deploy-to-kubernetes-raw-manifests)
- [6. Configuration reference](#6-configuration-reference)
- [7. Wiring credentials for /charts](#7-wiring-credentials-for-charts)
- [8. First install via /charts (smoke test)](#8-first-install-via-charts-smoke-test)
- [9. Tear down](#9-tear-down)
- [10. Troubleshooting](#10-troubleshooting)

---

## 1. Prerequisites

Required regardless of how you run KubeShipper:

- **Kubernetes 1.27+** target cluster (the SDK is tested against k8s.io/client-go v0.30)
- **kubeconfig** with permission to install RBAC, ServiceAccounts, Deployments, PVCs in your target namespace
- **A default StorageClass** if you'll deploy KubeShipper itself into the cluster (it uses a 1 Gi PVC for SQLite)

Optional, depending on what you'll do:

| Want to … | Also need … |
|---|---|
| Build / run locally | Go 1.22+ |
| Build the container image | Docker (or any OCI builder — buildah, nerdctl, etc.) |
| Use the `/charts` API | Cluster-admin on the cluster (see Section 4 + `rbac.helmAdmin`) |
| Install charts that need TLS via cert-manager | cert-manager + Traefik (or similar Ingress) installed in the cluster |
| Install charts whose ClusterIssuer uses Cloudflare DNS01 | A Cloudflare API token Secret — KubeShipper can provision this for you (see Section 7) |

---

## 2. Local development

The fastest loop is `go run` against a real cluster (your `~/.kube/config`).

```bash
# Clone (or already cloned)
cd kubeshipper

# Resolve deps once
go mod tidy

# Configure
cp .env.example .env
# Edit .env — at minimum set MANAGED_NAMESPACES

# Run
MANAGED_NAMESPACES=default go run .
```

Open another terminal:

```bash
curl http://localhost:3000/health
# {"started_at":"...","status":"ok","version":"dev"}

curl http://localhost:3000/charts
# {"releases":[ ... live Helm releases from your cluster ... ]}
```

Quick `/services` test (creates a real Deployment + Service and streams the rollout):

```bash
RESP=$(curl -s -X POST http://localhost:3000/services \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "demo-echo",
    "image": "ealen/echo-server:latest",
    "port": 80,
    "replicas": 1
  }')
echo "$RESP"
# {"id":"demo-echo","jobId":"<uuid>","status":"PENDING","stream":"/services/jobs/<uuid>/stream"}

# Stream the rollout to completion
JOB=$(echo "$RESP" | jq -r .jobId)
curl -N "http://localhost:3000/services/jobs/$JOB/stream"

# Confirm in the cluster
kubectl get deploy,svc -l app=demo-echo

# Tear down (also returns a streaming job)
DRESP=$(curl -s -X DELETE http://localhost:3000/services/demo-echo)
curl -N "http://localhost:3000$(echo "$DRESP" | jq -r .stream)"
```

### Build the binary directly

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ./bin/kubeshipper .
./bin/kubeshipper      # honors PORT, DB_PATH, MANAGED_NAMESPACES, etc.
```

Static binary, no CGO required (modernc.org/sqlite is pure Go).

---

## 3. Build the Docker image

```bash
docker build -t kubeshipper:local .
```

The Dockerfile is single-stage Go → alpine:3.20. Final image is ~54 MB. Runs as
non-root user `ks` (uid 1000).

Run locally with your kubeconfig mounted (testing only):

```bash
docker run --rm \
  -e AUTH_TOKEN=dev \
  -e MANAGED_NAMESPACES=default \
  -v ~/.kube:/home/ks/.kube:ro \
  -p 3000:3000 \
  kubeshipper:local
```

For CI: `.github/workflows/build-push-gcr.yml` builds and pushes to GHCR on
every push to `main` and on `v*` tags. Image goes to
`ghcr.io/aerol-ai/kubeshipper`, chart to `oci://ghcr.io/aerol-ai/helm/kubeshipper`.

---

## 4. Deploy to Kubernetes (Helm — recommended)

### 4.1 Minimal install (services-only, no /charts)

```bash
helm install kubeshipper ./helm-chart \
  --namespace kubeshipper --create-namespace \
  --set image.repository=ghcr.io/aerol-ai/kubeshipper \
  --set image.tag=latest \
  --set auth.token=$(openssl rand -hex 32) \
  --set rbac.managedNamespaces[0]=default
```

This grants the SA only the narrow per-namespace RBAC for `/services`. `/charts`
will be reachable but most charts will fail with permission errors.

### 4.2 Install with /charts enabled

`/charts` needs cluster-admin because Helm charts can include CRDs, ClusterIssuers,
and resources outside `managedNamespaces`. Set `rbac.helmAdmin=true`:

```bash
helm install kubeshipper ./helm-chart \
  --namespace kubeshipper --create-namespace \
  --set image.repository=ghcr.io/aerol-ai/kubeshipper \
  --set image.tag=latest \
  --set auth.token=$(openssl rand -hex 32) \
  --set rbac.helmAdmin=true \
  --set rbac.managedNamespaces[0]=default \
  --set rbac.managedNamespaces[1]=production
```

> ⚠️ With `helmAdmin=true`, anyone holding `auth.token` can install any Helm
> chart, which can create any Kubernetes resource. The token is effectively
> cluster-admin. Rotate it like one.

### 4.3 Install from the OCI-published chart (no clone)

```bash
helm install kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --version 0.1.1 \
  --namespace kubeshipper --create-namespace \
  --set auth.token=$(openssl rand -hex 32) \
  --set rbac.helmAdmin=true \
  --set rbac.managedNamespaces[0]=default
```

### 4.4 Install with access to all namespaces

To let kubeshipper deploy `/services` workloads into **any** namespace (no
upfront list), use the default `rbac.clusterWide=true`. This creates a
`ClusterRole` + `ClusterRoleBinding` instead of per-namespace `Role`s, so
`rbac.managedNamespaces` is not needed for `/services` RBAC.

```bash
helm install kubeshipper oci://ghcr.io/aerol-ai/helm/kubeshipper \
  --version 0.1.1 \
  --namespace kubeshipper --create-namespace \
  --set auth.token=$(openssl rand -hex 32) \
  --set rbac.helmAdmin=true \
  --set rbac.clusterWide=true
```

> ⚠️ Cluster-wide mode grants the SA deployments/services/pods/ingresses/jobs
> permissions across the whole cluster. Combined with `helmAdmin=true` (which
> binds `cluster-admin` for `/charts`), the `auth.token` is effectively
> cluster-admin. Use namespace-scoped mode (§4.5) for tighter blast radius.

### 4.5 Verify

```bash
kubectl -n kubeshipper get pod
kubectl -n kubeshipper logs -l app.kubernetes.io/name=kubeshipper -f

# Reach the API (port-forward for testing)
kubectl -n kubeshipper port-forward svc/kubeshipper 3000:3000
curl -H "Authorization: Bearer $TOKEN" http://localhost:3000/health
```

### 4.6 Upgrade

```bash
helm upgrade kubeshipper ./helm-chart \
  --namespace kubeshipper \
  --set image.tag=$(git rev-parse --short HEAD) \
  --reuse-values
```

---

## 5. Deploy to Kubernetes (raw manifests)

Use this only if you can't use Helm. The raw manifests in `k8s/` only cover
`/services`-style RBAC — extend RBAC manually if you want `/charts`.

```bash
kubectl create namespace kubeshipper
kubectl apply -f k8s/rbac.yaml          # cluster-wide /services RBAC
kubectl apply -f k8s/deployment.yaml    # edit env vars first

# (Optional) Add a cluster-admin binding for /charts:
kubectl create clusterrolebinding kubeshipper-helm-admin \
  --clusterrole=cluster-admin \
  --serviceaccount=default:kubeshipper
```

---

## 6. Configuration reference

### Environment variables (read by the binary)

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `3000` | HTTP listen port |
| `DB_PATH` | `kubeshipper.sqlite` | SQLite file path. Mount on a PVC in K8s. |
| `MANAGED_NAMESPACES` | _(required)_ | Comma-separated namespaces the **`/services`** API may deploy into. Server refuses to start if unset. |
| `AUTH_TOKEN` | _(unset)_ | Bearer token. Empty = no auth. Required header: `Authorization: Bearer <token>`. |
| `APP_VERSION` | `dev` | Returned by `/health`. CI usually sets this to a git SHA. |
| `KUBECONFIG` | _(unset)_ | Path to a kubeconfig file. Falls back to in-cluster service account. |

### Helm chart values (written to env above)

| Path | Default | Sets |
|---|---|---|
| `auth.token` | `""` | `AUTH_TOKEN` (via Secret) |
| `auth.existingSecret` | `""` | Use a pre-existing K8s Secret (key `auth-token`) |
| `image.repository` | `ghcr.io/aerol-ai/kubeshipper` | Container image |
| `image.tag` | `""` | Image tag (defaults to chart appVersion) |
| `storage.size` | `1Gi` | PVC size for SQLite |
| `storage.storageClass` | `""` | StorageClass (empty = cluster default) |
| `storage.mountPath` | `/data` | Where SQLite lives in the pod |
| `rbac.clusterWide` | `true` | `/services` RBAC: ClusterRole vs per-namespace Role |
| `rbac.managedNamespaces` | `["default"]` | Drives `MANAGED_NAMESPACES` |
| `rbac.helmAdmin` | `false` | Bind SA to `cluster-admin` so `/charts` can install arbitrary charts |
| `replicaCount` | `1` | **Do not increase** — SQLite single-writer constraint |

---

## 7. Wiring credentials for /charts

Credentials are **per-request, never persisted**. The audit log hashes the body
after redacting sensitive fields. There is no global "registry credentials"
configuration.

### Private GHCR (OCI)

Create a GitHub PAT with `read:packages` scope, then:

```json
{
  "source": {
    "type": "oci",
    "url": "oci://ghcr.io/Penify-dev/aerol-stack",
    "version": "0.1.0",
    "auth": { "username": "your-gh-user", "password": "ghp_xxx" }
  }
}
```

### Private HTTPS Helm repo

```json
{
  "source": {
    "type": "https",
    "repoUrl": "https://charts.acme.com",
    "chart": "my-chart",
    "version": "1.2.3",
    "auth": { "username": "ci", "password": "..." }
  }
}
```

### Private git repo (HTTPS PAT)

```json
{
  "source": {
    "type": "git",
    "repoUrl": "https://github.com/Penify-dev/aerol-helm-chart.git",
    "ref": "main",
    "path": "aerol-stack",
    "auth": { "token": "ghp_xxx" }
  }
}
```

### Private git repo (SSH)

```json
{
  "source": {
    "type": "git",
    "repoUrl": "git@github.com:Penify-dev/aerol-helm-chart.git",
    "ref": "v0.1.0",
    "path": "aerol-stack",
    "auth": { "sshKeyPem": "-----BEGIN OPENSSH PRIVATE KEY-----\n...\n" }
  }
}
```

### Uploaded `.tgz`

```bash
TGZ=$(base64 < aerol-stack-0.1.0.tgz | tr -d '\n')
curl -X POST http://kubeshipper/charts \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg b "$TGZ" '{
    release: "aerol-stack",
    namespace: "aerol-system",
    source: { type: "tgz", tgzBase64: $b }
  }')"
```

### Provisioning prerequisite secrets in one shot

If a chart's ClusterIssuer references `cloudflare-api-token-secret` in
`cert-manager` namespace, KubeShipper can create it for you on first install:

```json
{
  "release": "aerol-stack",
  "namespace": "aerol-system",
  "source": { ... },
  "prerequisites": {
    "secrets": [
      {
        "namespace": "cert-manager",
        "name": "cloudflare-api-token-secret",
        "type": "Opaque",
        "stringData": { "api-token": "cf_xxxxx" }
      }
    ]
  }
}
```

The namespace is created if missing. Existing secrets are updated in place.

---

## 8. First install via /charts (smoke test)

```bash
TOKEN=...      # the auth.token you set
KS=http://localhost:3000

# 1. Pre-flight
curl -X POST $KS/charts/preflight \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "release": "aerol-stack",
    "namespace": "aerol-system",
    "source": {
      "type": "oci",
      "url": "oci://ghcr.io/Penify-dev/aerol-stack",
      "version": "0.1.0",
      "auth": {"username": "you", "password": "ghp_..."}
    }
  }' | jq .

# 2. Install
JOB=$(curl -X POST $KS/charts \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "release": "aerol-stack",
    "namespace": "aerol-system",
    "source": { ...same as above... }
  }' | jq -r .jobId)

# 3. Stream progress
curl -N -H "Authorization: Bearer $TOKEN" $KS/charts/jobs/$JOB/stream

# 4. Inspect once done
curl -H "Authorization: Bearer $TOKEN" "$KS/charts/aerol-stack?namespace=aerol-system" | jq .
```

---

## 9. Tear down

### Uninstall a Helm release managed by /charts

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  "$KS/charts/aerol-stack?namespace=aerol-system&force=true"
```

PVCs labeled `app.kubernetes.io/instance=<release>` are swept automatically.

### Uninstall KubeShipper itself

```bash
helm uninstall kubeshipper --namespace kubeshipper
kubectl delete namespace kubeshipper
# If you turned helmAdmin on:
kubectl delete clusterrolebinding kubeshipper-helm-admin || true
```

The PVC is deleted with the release; its data is gone. To preserve, set
`storage.persistence.preserve` (TODO — currently always deleted).

---

## 10. Troubleshooting

**Server refuses to start: `MANAGED_NAMESPACES is not set`**
You must set it. Empty string is also rejected. Use a comma list: `default,production`.

**`/charts` returns `forbidden` 403 errors when installing**
The SA isn't bound to `cluster-admin`. Set `rbac.helmAdmin=true` and re-deploy
KubeShipper, or apply the binding manually (Section 5).

**`/charts/preflight` says `crd:certificates.cert-manager.io installed: false`**
The chart needs cert-manager CRDs. Install cert-manager first:
```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.18.0/cert-manager.yaml
```

**`/charts/preflight` says `default-storage-class: no default StorageClass found`**
The chart has PVCs but no default StorageClass exists. Mark one as default:
```bash
kubectl patch storageclass <name> -p '{"metadata":{"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
```

**SSE stream stops abruptly with no `complete` event**
The HTTP client likely timed out. Reconnect to `/charts/jobs/<jobId>/stream`
— the server replays everything from the persisted `events_jsonl` then continues
live.

**SQLite locked errors after a crash**
The DB is WAL-mode. If you see "database is locked", check that nothing else
opened `DB_PATH`. There's only one writer (KubeShipper). Delete the `*-shm`
and `*-wal` siblings and restart only if you're sure no other process owns it.

**Drift detected on every upgrade**
Something is editing your resources outside Helm — another operator, a
mutating webhook, or a person with `kubectl edit`. Investigate that source;
KubeShipper will auto-resync but the drift will keep coming back.

# /charts API — Helm chart management

The `/charts` API drives the Helm v3 Go SDK in-process and exposes
install / upgrade / uninstall / rollback / disable-resource over HTTP, with
Server-Sent Events for progress.

Helm release state stays in Helm itself (release Secrets in-cluster) — the
`/charts` API never caches release manifests. SQLite holds only:

- `jobs` — in-flight operation logs (events_jsonl + status, used for SSE replay)
- `disabled_resources` — post-renderer ledger that filters per-resource
- `chart_audit` — append-only audit trail with credentials redacted

## Architecture

```
client ──HTTP/SSE──> kubeshipper (Go binary) ──> Helm SDK ──> kube API
                          │
                          └── SQLite (jobs, disabled, audit)
```

One process. No sidecar, no gRPC, no proto file. Helm SDK is a Go library
imported by the same binary that serves HTTP.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/charts` | Install (returns 202 + jobId + SSE URL) |
| `GET`  | `/charts?namespace=&all=` | Live list from Helm |
| `POST` | `/charts/preflight` | Run checks without installing |
| `GET`  | `/charts/:release?namespace=` | Release detail + values + manifest + disabled list |
| `PATCH`| `/charts/:release?namespace=` | Upgrade (auto drift-resync) |
| `DELETE` | `/charts/:release?namespace=&force=true` | Uninstall + reap PVCs |
| `POST` | `/charts/:release/rollback?namespace=` | Roll back to revision |
| `GET`  | `/charts/:release/history?namespace=` | Revisions |
| `GET`  | `/charts/:release/values?namespace=` | Applied values |
| `GET`  | `/charts/:release/manifest?namespace=` | Rendered manifests |
| `POST` | `/charts/:release/resources/:kind/:name/disable?namespace=&force=true` | Strip a single resource via post-renderer |
| `POST` | `/charts/:release/resources/:kind/:name/enable?namespace=` | Re-add a stripped resource |
| `DELETE` | `/charts/:release/resources/:kind/:name?namespace=&force=true` | Alias for disable |
| `GET`  | `/charts/jobs/:jobId` | Job status + accumulated events |
| `GET`  | `/charts/jobs/:jobId/stream` | Server-Sent Events |

## Required RBAC

Charts can include cluster-scoped resources (CRDs, ClusterIssuer, ClusterRoleBinding),
multi-namespace resources, and Namespace objects themselves. Bind the SA to
`cluster-admin`:

```bash
helm install kubeshipper ./helm-chart --set rbac.helmAdmin=true ...
```

There is no narrower role that reliably covers arbitrary Helm charts. If you
only use `/services`, leave `helmAdmin` off — the default RBAC is fine.

## Install request body

```json
{
  "release": "aerol-stack",
  "namespace": "aerol-system",
  "source": {
    "type": "oci",
    "url": "oci://ghcr.io/Penify-dev/aerol-stack",
    "version": "0.1.0",
    "auth": { "username": "github-user", "password": "ghp_xxx" }
  },
  "values": { "auto-coder": { "replicas": 3 } },
  "atomic": true,
  "wait": true,
  "timeoutSeconds": 600,
  "createNamespace": true,
  "rolloutWatch": {
    "deployment": "agent-gateway"
  },
  "prerequisites": {
    "secrets": [
      {
        "namespace": "cert-manager",
        "name": "cloudflare-api-token-secret",
        "type": "Opaque",
        "stringData": { "api-token": "<cf-token>" }
      }
    ]
  }
}
```

`source.type` is one of:

| Type | Required fields | Notes |
|---|---|---|
| `oci` | `url`, `version` | `url` must start with `oci://`. `auth` for private registries. |
| `https` | `repoUrl`, `chart`, `version` | Classic Helm repos (e.g. `https://charts.bitnami.com/bitnami`). `auth` optional. |
| `git` | `repoUrl`, `ref` | `path` for sub-directory charts. `auth.token` for HTTPS, `auth.sshKeyPem` for SSH. |
| `tgz` | `tgzBase64` | Base64-encoded tarball. Use for charts not published anywhere. |

Credentials are per-request and never persisted. The audit log hashes the
request body after redacting `password`, `token`, `sshKeyPem`, `tgzBase64`,
`stringData`, and `auth` fields.

`prerequisites.secrets[]` are provisioned (idempotently) before the chart is
installed — useful for things like `cloudflare-api-token-secret` that the
chart's ClusterIssuer references but does not create itself.

`rolloutWatch` is optional. When provided, KubeShipper will register or refresh
the rollout watch immediately after a successful install or upgrade, so the
same request both deploys the chart and wires the 60-second digest watcher.

Fields:

| Field | Required | Notes |
|---|---|---|
| `deployment` | one of `deployment` / `service` | Deployment name to watch |
| `service` | one of `deployment` / `service` | Alias for callers that think in service names |
| `container` | no | Required only when the Deployment has multiple containers |

The `PATCH /charts/:release` upgrade request accepts the same `rolloutWatch`
block, letting each chart upgrade refresh the watch registration without a
separate `/rollout-watches` call.

## Response (202 Accepted)

```json
{
  "jobId": "5d3a8f1b...",
  "release": "aerol-stack",
  "namespace": "aerol-system",
  "stream": "/charts/jobs/5d3a8f1b.../stream",
  "status": "pending"
}
```

Open the `stream` URL with `curl -N` or an EventSource client to watch progress
in real time.

## SSE event format

Each event is a single line beginning with `data:` followed by a JSON object.
Phases: `validation`, `prereqs`, `apply`, `wait`, `done`, `error`, `complete`.

```
data: {"phase":"validation","message":"fetching chart","ts":1714155600000}
data: {"phase":"prereqs","message":"provisioning 1 prerequisite secret(s)","ts":...}
data: {"phase":"apply","message":"installing aerol-system/aerol-stack ...","ts":...}
data: {"phase":"done","message":"revision=1 status=deployed","ts":...}
data: {"phase":"complete","message":"succeeded","ts":...}
event: end
data: {"status":"succeeded"}
```

The same events are also persisted to `jobs.events_jsonl`, so a client that
disconnects can re-connect to the same `jobId/stream` URL and replay everything
from the beginning, then continue live.

## Drift handling

On `PATCH /charts/:release`, KubeShipper compares Helm's stored manifest to the
live cluster. If any rendered resource is missing in the cluster, that's drift.

Behavior:
1. A `drift-resync` job is created that re-applies Helm's desired state with
   `--reuse-values --atomic --wait`
2. Once resync settles, the user's upgrade proceeds normally
3. If resync fails, the upgrade returns 409 with the diff payload

Field-level diffing is intentionally out of scope for v1 — `kubectl edit` on a
managed Deployment will not be detected. Presence-only is sufficient for the
common case (`kubectl delete` outside Helm).

## Disable / enable resources

Helm has no native partial uninstall. KubeShipper uses Helm's PostRenderer to
filter rendered manifests against a `disabled_resources` ledger.

```bash
# Disable just langfuse-worker (force flag required)
curl -X POST 'http://kubeshipper/charts/aerol-stack/resources/Deployment/langfuse-worker/disable?namespace=aerol-system&force=true' \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "source": { "type": "oci", "url": "oci://ghcr.io/Penify-dev/aerol-stack", "version": "0.1.0", "auth": {...} },
    "resourceNamespace": "aerol-system",
    "deletePvcs": true
  }'
```

What happens:
1. KubeShipper writes a row into `disabled_resources`
2. Triggers `helm upgrade --reuse-values --atomic` with the post-renderer attached
3. Post-renderer drops every manifest that matches the ledger (kind+name, optionally namespace)
4. Helm reconciles; the disabled resource is deleted by Helm itself
5. If `deletePvcs: true`, KubeShipper sweeps PVCs that look bound to the resource:
   - StatefulSet: PVCs with name containing `-<sts>-`
   - Deployment: PVCs labeled `app=<deployment-name>`

Re-enable removes the row and triggers another upgrade.

## Force flag

Destructive operations require `?force=true` in the query string. Without it
the API returns 400. This is a friction step, not a security boundary —
`AUTH_TOKEN` is the security boundary.

| Op | force needed |
|---|---|
| `DELETE /charts/:release` (uninstall) | yes |
| `POST .../resources/.../disable` | yes |
| `DELETE .../resources/.../...` (alias for disable) | yes |
| `POST .../resources/.../enable` | no |
| `PATCH /charts/:release` (upgrade) | no |
| `POST /charts/:release/rollback` | no |

## Building

```bash
# Local (requires Go 1.22+):
go mod tidy
go run .

# Container:
docker build -t kubeshipper:latest .
```

The Dockerfile is a single-stage Go build into `alpine:3.20` — no Bun, no
`protoc`, no helmd sidecar. Final image is ~54 MB with the binary linked statically.

## Security notes

- `/charts` requires the SA to be bound to `cluster-admin` (set
  `rbac.helmAdmin=true` in the chart). The `AUTH_TOKEN` therefore grants
  cluster-admin-equivalent power to anyone holding it. Treat it accordingly.
- Credentials are never written to disk. The audit log SHA-256 hashes the
  request body after redacting sensitive keys.
- Auto drift-resync re-applies Helm's stored manifest. If a drifted state
  exists *because* somebody intentionally edited a resource, the resync will
  overwrite it. Treat any out-of-band kubectl edits as ephemeral.

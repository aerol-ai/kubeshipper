# /charts API — Helm chart management

The `/charts` API wraps the Helm v3 Go SDK via a sidecar (`helmd`) and exposes
install / upgrade / uninstall / disable-resource over HTTP with SSE progress.

Helm release state is the single source of truth — KubeShipper does **not**
cache release manifests in SQLite. SQLite holds only:

- `jobs` — in-flight operation logs (for SSE replay)
- `disabled_resources` — post-renderer ledger
- `chart_audit` — append-only audit trail

## Architecture

```
client ──HTTP/SSE──> bun (Hono) ──gRPC over UDS──> helmd (Go) ──> Helm SDK ──> kube
                          │                                                       │
                          └───── SQLite (jobs, disabled, audit) ──────────────────┘
```

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/charts` | Install (returns 202 + jobId + SSE URL) |
| `GET` | `/charts?namespace=&all=` | Live list from Helm |
| `POST` | `/charts/preflight` | Run checks without installing |
| `GET` | `/charts/:release?namespace=` | Release detail + values + manifest + disabled list |
| `PATCH` | `/charts/:release?namespace=` | Upgrade (auto drift-resync) |
| `DELETE` | `/charts/:release?namespace=&force=true` | Uninstall + reap PVCs |
| `POST` | `/charts/:release/rollback?namespace=` | Roll back to revision |
| `GET` | `/charts/:release/history?namespace=` | Revisions |
| `GET` | `/charts/:release/values?namespace=` | Applied values |
| `GET` | `/charts/:release/manifest?namespace=` | Rendered manifests |
| `POST` | `/charts/:release/resources/:kind/:name/disable?namespace=&force=true` | Strip a single resource via post-renderer |
| `POST` | `/charts/:release/resources/:kind/:name/enable?namespace=` | Re-add a stripped resource |
| `DELETE` | `/charts/:release/resources/:kind/:name?namespace=&force=true` | Alias for disable |
| `GET` | `/charts/jobs/:jobId` | Job status + accumulated events |
| `GET` | `/charts/jobs/:jobId/stream` | Server-Sent Events |

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

`source.type` can be `oci`, `https`, `git`, or `tgz` (base64-encoded bytes).
Credentials are per-request and never persisted. The audit log hashes the
request after redacting credentials.

## SSE event format

Each event is a JSON object with `phase`, `message`, optional resource hints,
and `ts` (unix millis):

```
data: {"phase":"validation","message":"fetching chart","ts":1714155600000}
data: {"phase":"prereqs","message":"provisioning 1 prerequisite secret(s)","ts":...}
data: {"phase":"apply","message":"installing aerol-system/aerol-stack","ts":...}
data: {"phase":"done","message":"revision=1 status=deployed","ts":...}
event: end
data: {"status":"succeeded"}
```

## Drift handling

On `PATCH /charts/:release`, KubeShipper diffs Helm's stored manifest against
the live cluster. If drift is detected, it emits a `drift-resync` job that
re-applies Helm's desired state, waits for it to settle, then proceeds with
the user's upgrade. If resync fails, the upgrade returns 409 with the diff.

## Disable / enable resources

Helm has no native partial uninstall. KubeShipper uses Helm's PostRenderer to
filter rendered manifests against a `disabled_resources` ledger:

1. `POST /charts/aerol-stack/resources/Deployment/langfuse-worker/disable?namespace=aerol-system&force=true`
2. KubeShipper records the row and triggers `helm upgrade --reuse-values`
3. The post-renderer drops `Deployment/langfuse-worker` from the rendered set
4. Helm reconciles; the resource is deleted by Helm itself
5. PVCs labeled with the resource are swept (controllable per request)

Re-enable removes the row and triggers another upgrade.

## Building

```bash
# Local (requires Go toolchain + protoc + bun):
cd helmd && go mod tidy
protoc --go_out=helmd/gen --go-grpc_out=helmd/gen \
       --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative \
       -I helmd/proto helmd/proto/helmd.proto

# Container:
docker build -t kubeshipper:latest .
```

## Security notes

- `/charts` runs as a SA bound to `cluster-admin` (Helm charts can include any
  cluster-scoped resource). Tighten only if you know what your charts contain.
- Credentials are never written to disk. The audit log redacts `password`,
  `token`, `sshKeyPem`, `tgzBase64`, `stringData`, `auth` fields before hashing.
- The helmd UDS is `0600` and only reachable inside the pod.

# Use cases

Concrete scenarios the current codebase supports. Every use case maps to live
code paths — none are aspirational. Where useful, the example shows the exact
HTTP call that triggers it.

Variables used throughout:

```bash
KS=http://localhost:3000/api
TOKEN=...      # AUTH_TOKEN if set, else any value (auth is bypassed)
```

---

## /api/services — JSON-spec deployments

These exercise the `/services` API and the worker loop in
`internal/worker/worker.go`. The lifecycle state machine is:

```
PENDING → DEPLOYING → READY            (happy path)
                ↘   FAILED             (no previous READY spec)
                ↘   PENDING (rolled-back to last_ready_spec_json)
READY   → PENDING                       (drift: Deployment missing in K8s)
```

### UC-1. Deploy a stateless HTTP service (streaming)

Every mutating `/services` call returns a `jobId` + SSE stream URL. The example
deploys a service then streams the rollout to completion in one shot.

**Smoke test — minimal payload (no resource limits, BestEffort QoS):**

```bash
RESP=$(curl -s -X POST $KS/services \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "echo",
    "image": "ealen/echo-server:latest",
    "port": 80,
    "replicas": 2,
    "public": true,
    "hostname": "echo.example.com",
    "namespace": "default"
  }')
# 202 → {"id":"echo","jobId":"<id>","status":"PENDING","stream":"/api/services/jobs/<id>/stream"}

JOB=$(echo "$RESP" | jq -r .jobId)
curl -N -H "Authorization: Bearer $TOKEN" "$KS$(echo "$RESP" | jq -r .stream)"
```

Output (one event per worker phase transition):

```
data: {"phase":"validation","message":"Service create requested via API","ts":...}
data: {"phase":"apply","message":"Deploying: Worker picked up deployment task and started SSA","ts":...}
data: {"phase":"done","message":"RolloutComplete: Deployment rollout successfully finished and is Ready","ts":...}
data: {"phase":"complete","message":"succeeded","ts":...}
```

`resources` is intentionally omitted so the example fits on one screen. The
pod gets **no CPU/memory requests, no limits, BestEffort QoS** — fine for a
smoke test, **not** what you want in production. With no requests, the
scheduler treats CPU/memory reservation as zero; with no limits, the container
can burst until OOM-killed by the kernel under node pressure. BestEffort pods
are the first to be evicted.

**Production-shaped — set requests and limits:**

```bash
curl -X POST $KS/services \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "echo",
    "image": "ealen/echo-server:latest",
    "port": 80,
    "replicas": 2,
    "public": true,
    "hostname": "echo.example.com",
    "namespace": "default",
    "resources": {
      "requests": { "cpu": "50m",  "memory": "64Mi" },
      "limits":   { "cpu": "200m", "memory": "256Mi" }
    }
  }'
```

`requests` is what the scheduler reserves; `limits` is the hard cap. Values are
parsed by Kubernetes' `resource.ParseQuantity` — use standard suffixes
(`m` = milli-CPU, `Mi` / `Gi` = mebi/gibi-bytes). Malformed quantities are
silently dropped (see `internal/kube/adapter.go:parseRequests`).

### Phase semantics for `/services` jobs

| Phase | Trigger |
|---|---|
| `validation` | Job created (request accepted) |
| `apply` | Worker started SSA (`Deploying`) or initiated rollback (`AutoRollback`) |
| `wait` | Soft failure detected; rollback may run (`RolloutFailed`) |
| `done` | Operation succeeded (`RolloutComplete`, `service torn down`, etc.) — terminal: succeeded |
| `error` | Hard failure (`DeployFailed`/`RollbackWarning`) — terminal: failed |
| `complete` | Final marker emitted by the job runtime |

Reconnecting to the same `/services/jobs/:id/stream` URL replays everything
from `events_jsonl` then continues live — useful for clients that disconnect
mid-deploy.

### UC-2. Update a service spec (rolling deploy)

```bash
RESP=$(curl -s -X PATCH $KS/services/echo \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "image": "ealen/echo-server:0.9.2", "replicas": 3 }')
curl -N -H "Authorization: Bearer $TOKEN" "$KS$(echo "$RESP" | jq -r .stream)"
```

The merge logic in `internal/kube/spec.go` preserves any field the patch omits.
The worker re-applies via SSA and Kubernetes does a rolling update; the same
phase sequence as UC-1 streams from the new job.

### UC-3. Restart a service without changing the spec

Useful when the image tag is mutable (`:latest`) and the registry has a fresh
push.

```bash
RESP=$(curl -s -X POST $KS/services/echo/restart -H "Authorization: Bearer $TOKEN")
curl -N -H "Authorization: Bearer $TOKEN" "$KS$(echo "$RESP" | jq -r .stream)"
```

Adds a `kubeshipper.io/restartedAt` pod-template annotation via strategic merge
patch — Kubernetes treats that as a spec change and rolls the pods. Stream
emits `validation → done` once the patch is applied; the actual rollout watch
is left to the client (Kubernetes drives it).

### UC-4. Stream live pod logs

```bash
curl -N $KS/services/echo/logs -H "Authorization: Bearer $TOKEN"
```

Streams the first matching pod's `app` container with `tailLines=50` and
`follow=true`. Connection persists until the client disconnects.

### UC-5. Inspect a service + live K8s state

```bash
curl $KS/services/echo -H "Authorization: Bearer $TOKEN" | jq .
```

Returns the stored spec, recorded status, current `job_id` (if any operation
is in flight), and a live `k8sStatus` overlay (`readyReplicas`, conditions).

### UC-6. Replay a previous deploy/patch's event log

There's no separate "events" endpoint — every lifecycle event is on the job.
To inspect what happened on a past deploy, hit the job:

```bash
curl $KS/services/jobs/<jobId> -H "Authorization: Bearer $TOKEN" | jq .events
```

Returns the full `events_jsonl` for that job (validation, apply, done/error,
complete) plus operation type and start/end timestamps.

### UC-7. Tear down a service

```bash
RESP=$(curl -s -X DELETE $KS/services/echo -H "Authorization: Bearer $TOKEN")
curl -N -H "Authorization: Bearer $TOKEN" "$KS$(echo "$RESP" | jq -r .stream)"
```

Deletes Deployment + Service + Ingress in the configured namespace and removes
the row from the local DB. Stream emits `validation → done → complete` once
all three K8s deletes return.

### UC-8. Auto-rollback on a bad rollout

If a new spec triggers `ProgressDeadlineExceeded` or `ReplicaFailure`,
`internal/worker/worker.go:watchDeploying` detects it and re-applies the last
known `READY` spec (`last_ready_spec_json`). The same job stays attached
across the rollback, so a single SSE stream shows the full arc:

```
phase: apply  (Deploying)
phase: wait   (RolloutFailed)
phase: apply  (AutoRollback)
phase: done   (RolloutComplete)         ← rollback succeeded
phase: complete (succeeded)
```

If there's no previous READY spec to revert to, the stream terminates with
`error / RollbackWarning` instead.

### UC-9. Drift reconciliation

Every 60s, the worker walks all `READY` services and confirms the underlying
Deployment still exists. If it's gone (deleted out-of-band), the service is
flipped back to `PENDING` and the worker re-applies the spec on the next tick.

No SSE events are emitted for drift handling — the original deploy job has
already terminated by the time drift is detected. Observability is via
`GET /services/:id` (the `status` field flips back to `PENDING`, then
`DEPLOYING`, then `READY`).

---

## /api/charts — Helm chart management

These exercise `internal/api/charts.go` and `internal/helm/*`. Source of truth
for release state is Helm's release Secrets, not SQLite.

Common request body shape (referenced as `$BODY` below):

```json
{
  "release": "aerol-stack",
  "namespace": "aerol-system",
  "source": {
    "type": "oci",
    "url": "oci://ghcr.io/Penify-dev/aerol-stack",
    "version": "0.1.0",
    "auth": { "username": "user", "password": "ghp_..." }
  }
}
```

### UC-10. Install a chart from a private OCI registry (GHCR)

```bash
JOB=$(curl -X POST $KS/charts \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "$BODY" | jq -r .jobId)
```

KubeShipper pulls the chart, runs `helm install --atomic --wait`, and emits SSE
progress at every phase.

### UC-11. Install a chart from a classic HTTPS Helm repo (e.g. Bitnami)

```bash
curl -X POST $KS/charts \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{
    "release": "redis",
    "namespace": "data",
    "source": {
      "type": "https",
      "repoUrl": "https://charts.bitnami.com/bitnami",
      "chart": "redis",
      "version": "21.2.13"
    }
  }'
```

Anonymous; add `auth.{username,password}` for private repos.

### UC-12. Install a chart directly from a git repository

```bash
curl -X POST $KS/charts \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{
    "release": "aerol-stack",
    "namespace": "aerol-system",
    "source": {
      "type": "git",
      "repoUrl": "git@github.com:Penify-dev/aerol-helm-chart.git",
      "ref": "v0.1.0",
      "path": "aerol-stack",
      "auth": { "sshKeyPem": "-----BEGIN OPENSSH PRIVATE KEY-----\n...\n" }
    }
  }'
```

`ref` falls back from branch → tag automatically. SSH key or HTTPS token.

### UC-13. Install an unpublished chart from an uploaded `.tgz`

```bash
TGZ=$(base64 < my-chart-0.1.0.tgz | tr -d '\n')
curl -X POST $KS/charts \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "$(jq -n --arg b "$TGZ" '{
    release: "my-chart",
    namespace: "default",
    source: { type: "tgz", tgzBase64: $b }
  }')"
```

Useful in CI: build chart, upload, install — no registry round-trip.

### UC-14. Pre-flight a chart before committing

```bash
curl -X POST $KS/charts/preflight \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "$BODY" | jq .
```

Renders the chart and reports per-check status:

- `chart-resolvable` — pulled and rendered (blocking)
- `crd:<name>` — required CRDs installed (blocking, e.g. cert-manager / Traefik)
- `default-storage-class` — present if chart has PVCs (blocking)
- `no-conflicting-release` — no existing release with same name in namespace (blocking)
- `dns:<host>` — DNS resolves (warning)

### UC-15. Auto-provision required secrets on first install

The chart's ClusterIssuer references `cloudflare-api-token-secret` but doesn't
create it — KubeShipper does, idempotently:

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

The target namespace is created if missing. Re-running with the same payload
updates the secret in place rather than failing.

### UC-16. Stream install progress in real time (SSE)

```bash
curl -N -H "Authorization: Bearer $TOKEN" \
  "$KS/charts/jobs/$JOB/stream"
```

Phases: `validation` → `prereqs` → `apply` → `done` → `complete`. The same
events are persisted; reconnecting to the same job replays from the start then
continues live.

### UC-17. Fetch a job's full state by ID

```bash
curl $KS/charts/jobs/$JOB -H "Authorization: Bearer $TOKEN" | jq .
```

Returns operation, status, started_at, ended_at, and the full event log. Useful
for non-streaming clients.

### UC-18. List all live releases (across namespaces)

```bash
curl $KS/charts -H "Authorization: Bearer $TOKEN" | jq '.releases[] | {name, namespace, status, revision}'
```

Optional filters: `?namespace=foo` (single ns), `?all=true` (include uninstalled).

### UC-19. Get release detail with values + manifest + disabled list

```bash
curl "$KS/charts/aerol-stack?namespace=aerol-system" -H "Authorization: Bearer $TOKEN" | jq .
```

Returns `release` (status, revision), `values_yaml` (applied values), `manifest`
(rendered YAML), and `disabled` (post-renderer ledger).

### UC-20. View revision history

```bash
curl "$KS/charts/aerol-stack/history?namespace=aerol-system" -H "Authorization: Bearer $TOKEN" | jq .
```

Last 20 revisions with chart, app version, status, deployed-at, description.

### UC-21. View applied values only

```bash
curl "$KS/charts/aerol-stack/values?namespace=aerol-system" -H "Authorization: Bearer $TOKEN" | jq -r .values_yaml
```

Equivalent to `helm get values <release>`.

### UC-22. View rendered manifest only

```bash
curl "$KS/charts/aerol-stack/manifest?namespace=aerol-system" -H "Authorization: Bearer $TOKEN"
```

Returns the manifest as `application/yaml`. Equivalent to `helm get manifest`.

### UC-23. Upgrade a release with new image tag override

```bash
curl -X PATCH "$KS/charts/aerol-stack?namespace=aerol-system" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{
    "source": { ... },
    "values": { "auto-coder": { "image": { "tag": "v1.4.2" } } }
  }'
```

Atomic + wait by default. Helm only rolls Deployments whose pod-spec actually
changed.

### UC-24. Auto-detect & resync drift before upgrade

If somebody ran `kubectl delete deployment auto-coder` outside Helm,
`PATCH /charts/aerol-stack` notices the missing resource (via `Diff()` in
`internal/helm/diff.go`), spawns a `drift-resync` job, re-applies Helm's
desired state, then proceeds with the user's upgrade.

Observable in the response:

```json
{ "jobId": "...", "stream": "...", "status": "pending" }
```

…and a separate job with operation `drift-resync` will appear in
`/api/charts/jobs/...`.

### UC-25. Roll back to a previous revision

```bash
curl -X POST "$KS/charts/aerol-stack/rollback?namespace=aerol-system" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{ "revision": 3, "wait": true }'
```

`revision: 0` means "previous". Synchronous (blocks until done); not streamed.

### UC-26. Disable a single resource within a release (post-renderer)

Strip just `Deployment/langfuse-worker` without uninstalling the rest:

```bash
curl -X POST \
  "$KS/charts/aerol-stack/resources/Deployment/langfuse-worker/disable?namespace=aerol-system&force=true" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{
    "source": { ... },
    "resourceNamespace": "aerol-system",
    "deletePvcs": true
  }'
```

Records the row in `disabled_resources`, runs `helm upgrade --reuse-values` with
the post-renderer attached, and (because `deletePvcs: true`) sweeps PVCs that
look bound to the resource (StatefulSet pattern: `data-<sts>-N`; Deployment
pattern: PVC labeled `app=<name>`).

### UC-27. Re-enable a previously disabled resource

```bash
curl -X POST \
  "$KS/charts/aerol-stack/resources/Deployment/langfuse-worker/enable?namespace=aerol-system" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{ "source": { ... } }'
```

Removes the row, re-runs upgrade, the resource comes back.

### UC-28. Force-uninstall a release with PVC sweep

```bash
curl -X DELETE \
  "$KS/charts/aerol-stack?namespace=aerol-system&force=true" \
  -H "Authorization: Bearer $TOKEN"
```

`force=true` is mandatory. Returns the list of PVCs that were also deleted (any
PVC labeled `app.kubernetes.io/instance=<release>` in any namespace).

### UC-29. Audit trail of operations

Every `/charts` mutating call appends a row to `chart_audit` (in
`internal/store/audit.go`) with:

- timestamp, initiator fingerprint, operation, release, namespace, outcome
- a SHA-256 hash of the request body **after** redacting `password`, `token`,
  `sshKeyPem`, `tgzBase64`, `stringData`, and `auth` keys

There is no public endpoint for the audit table — it's append-only forensic
storage. Inspect with `sqlite3 /data/kubeshipper.sqlite` if needed.

### UC-30. Multi-source mix: install one chart per source type concurrently

```bash
# OCI release
curl -X POST $KS/charts -d '{ "release": "aerol", ...source.type=oci... }' &

# HTTPS release
curl -X POST $KS/charts -d '{ "release": "redis", ...source.type=https... }' &

# Git release
curl -X POST $KS/charts -d '{ "release": "internal-tool", ...source.type=git... }' &
```

Different releases run in parallel. Same-release operations serialize on
`Manager.mu` to keep Helm's state consistent.

---

## Operational behaviors (no API call needed)

### UC-31. Crash recovery

If KubeShipper crashes mid-deploy, services stuck in `DEPLOYING` are
auto-reset to `PENDING` on next boot (`store.ResetStuckDeployments`). The
worker picks them up and re-applies.

### UC-32. Auth bypass for in-cluster probes

`/health` and `/` are never gated by `AUTH_TOKEN` — required for kubelet
liveness/readiness probes that can't carry a header.

### UC-33. Namespace allow-list enforcement

`/services` requests targeting a namespace not in `MANAGED_NAMESPACES` get 400
before any Kubernetes call. `/charts` is not constrained by the allow-list —
that's `cluster-admin`'s job (set `rbac.helmAdmin=true`).

### UC-34. Constant-time auth comparison

`internal/api/auth.go` uses `subtle.ConstantTimeCompare` so `AUTH_TOKEN`
matching isn't subject to timing oracles. (Not strictly necessary at this
threat model — the token is a static bearer, not a derivation key — but
costs nothing.)

### UC-35. Single-replica SQLite enforcement

The Helm chart sets `replicaCount: 1`. The worker assumes one writer. Running
two replicas would cause split-brain on the `services` table. Documented as
**do not increase**; horizontal scale-out requires migrating the store to
Postgres (out of scope for v1).

import { Hono } from "hono";
import { stream as honoStream } from "hono/streaming";
import {
  InstallSchema,
  UpgradeSchema,
  RollbackSchema,
  DisableResourceSchema,
  PreflightSchema,
} from "./validation";
import {
  helmList,
  helmGet,
  helmHistory,
  helmUninstall,
  helmRollback,
  helmRender,
  helmPreflight,
  helmDiff,
  helmInstallStream,
  helmUpgradeStream,
  helmDisableStream,
  helmEnableStream,
} from "./helmd-client";
import { createJob, appendEvent, setJobStatus, subscribe, getJob, getJobEvents } from "./jobs";
import { recordDisabled, clearDisabled, listDisabled } from "./disabled-store";
import { auditLog } from "./audit";
import { valuesToYAML } from "./yaml-helpers";

export const chartsRouter = new Hono();

// --- helpers ---------------------------------------------------------------

function requireForce(c: any): boolean {
  const fromQuery = c.req.query("force") === "true";
  return fromQuery;
}

function initiator(c: any): string | undefined {
  const h = c.req.header("Authorization");
  if (!h?.startsWith("Bearer ")) return undefined;
  // We don't keep the raw token — fingerprint it.
  return "token:" + h.slice(7).slice(0, 8);
}

function buildHelmdReq(parsed: any) {
  return {
    release: parsed.release,
    namespace: parsed.namespace,
    source: parsed.source,
    valuesYaml: valuesToYAML(parsed.values ?? {}),
    atomic: parsed.atomic ?? true,
    wait: parsed.wait ?? true,
    timeoutSeconds: parsed.timeoutSeconds ?? 600,
    createNamespace: parsed.createNamespace ?? true,
    prereqSecrets: (parsed.prerequisites?.secrets ?? []).map((s: any) => ({
      namespace: s.namespace,
      name: s.name,
      type: s.type ?? "Opaque",
      stringData: s.stringData,
    })),
  };
}

// --- endpoints -------------------------------------------------------------

// POST /charts → install
chartsRouter.post("/", async (c) => {
  const body = await c.req.json();
  const parsed = InstallSchema.safeParse(body);
  if (!parsed.success) {
    auditLog({
      operation: "install", release: body?.release ?? "?", namespace: body?.namespace ?? "?",
      outcome: "rejected", payload: body, initiator: initiator(c),
    });
    return c.json({ error: "Validation failed", details: parsed.error.format() }, 400);
  }

  const req = buildHelmdReq(parsed.data);
  const jobId = createJob(parsed.data.release, parsed.data.namespace, "install", initiator(c));
  auditLog({
    operation: "install", release: parsed.data.release, namespace: parsed.data.namespace,
    outcome: "accepted", payload: parsed.data, initiator: initiator(c),
  });

  // Detached worker — runs the streaming RPC and pumps events into the job log.
  runStreamingJob(jobId, () => helmInstallStream(req));

  return c.json(
    {
      jobId,
      release: parsed.data.release,
      namespace: parsed.data.namespace,
      stream: `/charts/jobs/${jobId}/stream`,
      status: "pending",
    },
    202
  );
});

// GET /charts → live list from Helm (no DB cache)
chartsRouter.get("/", async (c) => {
  const ns = c.req.query("namespace") ?? "";
  const all = c.req.query("all") === "true";
  try {
    const out = await helmList(ns, all);
    return c.json(out);
  } catch (err: any) {
    return c.json({ error: "helm list failed", details: err.message }, 500);
  }
});

// GET /charts/preflight → no install, just checks
chartsRouter.post("/preflight", async (c) => {
  const body = await c.req.json();
  const parsed = PreflightSchema.safeParse(body);
  if (!parsed.success) return c.json({ error: parsed.error.format() }, 400);
  try {
    const out = await helmPreflight({
      release: parsed.data.release,
      namespace: parsed.data.namespace,
      source: parsed.data.source,
      valuesYaml: valuesToYAML(parsed.data.values ?? {}),
    });
    return c.json(out);
  } catch (err: any) {
    return c.json({ error: "preflight failed", details: err.message }, 500);
  }
});

// GET /charts/jobs/:id → job status + accumulated events
chartsRouter.get("/jobs/:id", (c) => {
  const id = c.req.param("id");
  const row = getJob(id);
  if (!row) return c.json({ error: "job not found" }, 404);
  return c.json({ ...row, events: getJobEvents(id) });
});

// GET /charts/jobs/:id/stream → SSE
chartsRouter.get("/jobs/:id/stream", (c) => {
  const id = c.req.param("id");
  const row = getJob(id);
  if (!row) return c.json({ error: "job not found" }, 404);

  return honoStream(c, async (stream) => {
    c.header("Content-Type", "text/event-stream");
    c.header("Cache-Control", "no-cache");
    c.header("Connection", "keep-alive");

    // Replay any events already accumulated.
    for (const ev of getJobEvents(id)) {
      await stream.write(`data: ${JSON.stringify(ev)}\n\n`);
    }

    if (row.status === "succeeded" || row.status === "failed") {
      await stream.write(`event: end\ndata: {"status":"${row.status}"}\n\n`);
      return;
    }

    // Subscribe live.
    let resolve: () => void;
    const done = new Promise<void>((r) => (resolve = r));
    const unsub = subscribe(id, async (ev: any) => {
      try {
        await stream.write(`data: ${JSON.stringify(ev)}\n\n`);
        if (ev?.phase === "complete" || ev?.phase === "error") {
          unsub();
          resolve();
        }
      } catch {
        unsub();
        resolve();
      }
    });
    await done;
  });
});

// GET /charts/:release  (with ?namespace=...)
chartsRouter.get("/:release", async (c) => {
  const release = c.req.param("release");
  const namespace = c.req.query("namespace");
  if (!namespace) return c.json({ error: "namespace query param required" }, 400);
  try {
    const release_info = await helmGet(release, namespace);
    return c.json({
      ...release_info,
      disabled: listDisabled(release, namespace),
    });
  } catch (err: any) {
    return c.json({ error: "release not found", details: err.message }, 404);
  }
});

// PATCH /charts/:release → upgrade
chartsRouter.patch("/:release", async (c) => {
  const release = c.req.param("release");
  const namespace = c.req.query("namespace");
  if (!namespace) return c.json({ error: "namespace query param required" }, 400);

  const body = await c.req.json();
  const parsed = UpgradeSchema.safeParse(body);
  if (!parsed.success) return c.json({ error: parsed.error.format() }, 400);

  // Drift check before upgrade.
  try {
    const diff = await helmDiff(release, namespace);
    if (diff.drifted) {
      // Auto-resync once: re-apply the desired manifest by upgrading with --reuse-values.
      const resyncJob = createJob(release, namespace, "drift-resync", initiator(c));
      appendEvent(resyncJob, { phase: "validation", message: "drift detected; auto-resyncing", diff });
      const resyncReq = {
        release,
        namespace,
        source: parsed.data.source,
        valuesYaml: valuesToYAML(parsed.data.values ?? {}),
        atomic: true,
        wait: true,
        reuseValues: true,
        timeoutSeconds: 300,
      };
      let resyncFailed = false;
      for await (const ev of helmUpgradeStream(resyncReq)) {
        appendEvent(resyncJob, ev);
        if (ev.phase === "error") resyncFailed = true;
      }
      setJobStatus(resyncJob, resyncFailed ? "failed" : "succeeded");
      if (resyncFailed) {
        return c.json({ error: "drift resync failed", diff, resyncJobId: resyncJob }, 409);
      }
    }
  } catch (err: any) {
    // If diff itself fails (e.g. release doesn't exist) we let upgrade error normally.
  }

  const req = {
    release,
    namespace,
    source: parsed.data.source,
    valuesYaml: valuesToYAML(parsed.data.values ?? {}),
    atomic: parsed.data.atomic,
    wait: parsed.data.wait,
    timeoutSeconds: parsed.data.timeoutSeconds,
    reuseValues: parsed.data.reuseValues,
    resetValues: parsed.data.resetValues,
  };

  const jobId = createJob(release, namespace, "upgrade", initiator(c));
  auditLog({
    operation: "upgrade", release, namespace,
    outcome: "accepted", payload: parsed.data, initiator: initiator(c),
  });
  runStreamingJob(jobId, () => helmUpgradeStream(req));

  return c.json({ jobId, stream: `/charts/jobs/${jobId}/stream`, status: "pending" }, 202);
});

// DELETE /charts/:release → uninstall (force required)
chartsRouter.delete("/:release", async (c) => {
  const release = c.req.param("release");
  const namespace = c.req.query("namespace");
  if (!namespace) return c.json({ error: "namespace query param required" }, 400);
  if (!requireForce(c)) {
    return c.json({ error: "Destructive op requires ?force=true" }, 400);
  }
  try {
    const out = await helmUninstall(release, namespace, true);
    auditLog({
      operation: "uninstall", release, namespace,
      outcome: "accepted", initiator: initiator(c),
    });
    return c.json(out);
  } catch (err: any) {
    return c.json({ error: "uninstall failed", details: err.message }, 500);
  }
});

// POST /charts/:release/rollback
chartsRouter.post("/:release/rollback", async (c) => {
  const release = c.req.param("release");
  const namespace = c.req.query("namespace");
  if (!namespace) return c.json({ error: "namespace query param required" }, 400);

  const body = await c.req.json();
  const parsed = RollbackSchema.safeParse(body);
  if (!parsed.success) return c.json({ error: parsed.error.format() }, 400);

  try {
    const out = await helmRollback(release, namespace, parsed.data.revision);
    auditLog({
      operation: "rollback", release, namespace,
      outcome: "accepted", payload: parsed.data, initiator: initiator(c),
    });
    return c.json(out);
  } catch (err: any) {
    return c.json({ error: "rollback failed", details: err.message }, 500);
  }
});

// GET /charts/:release/history
chartsRouter.get("/:release/history", async (c) => {
  const release = c.req.param("release");
  const namespace = c.req.query("namespace");
  if (!namespace) return c.json({ error: "namespace query param required" }, 400);
  try {
    const out = await helmHistory(release, namespace);
    return c.json(out);
  } catch (err: any) {
    return c.json({ error: "history failed", details: err.message }, 500);
  }
});

// GET /charts/:release/values
chartsRouter.get("/:release/values", async (c) => {
  const release = c.req.param("release");
  const namespace = c.req.query("namespace");
  if (!namespace) return c.json({ error: "namespace query param required" }, 400);
  try {
    const out = await helmGet(release, namespace);
    return c.json({ valuesYaml: out.valuesYaml });
  } catch (err: any) {
    return c.json({ error: "values failed", details: err.message }, 500);
  }
});

// GET /charts/:release/manifest
chartsRouter.get("/:release/manifest", async (c) => {
  const release = c.req.param("release");
  const namespace = c.req.query("namespace");
  if (!namespace) return c.json({ error: "namespace query param required" }, 400);
  try {
    const out = await helmGet(release, namespace);
    c.header("Content-Type", "application/yaml");
    return c.body(out.manifest);
  } catch (err: any) {
    return c.json({ error: "manifest failed", details: err.message }, 500);
  }
});

// POST /charts/:release/resources/:kind/:name/disable
chartsRouter.post("/:release/resources/:kind/:name/disable", async (c) => {
  const release = c.req.param("release");
  const kind = c.req.param("kind");
  const name = c.req.param("name");
  const namespace = c.req.query("namespace");
  if (!namespace) return c.json({ error: "namespace query param required" }, 400);
  if (!requireForce(c)) return c.json({ error: "Destructive op requires ?force=true" }, 400);

  const body = await c.req.json();
  const parsed = DisableResourceSchema.safeParse(body);
  if (!parsed.success) return c.json({ error: parsed.error.format() }, 400);

  recordDisabled(release, namespace, kind, name, parsed.data.resourceNamespace ?? "");

  const jobId = createJob(release, namespace, `disable:${kind}/${name}`, initiator(c));
  auditLog({
    operation: "disable-resource", release, namespace,
    outcome: "accepted", payload: { kind, name, ...parsed.data }, initiator: initiator(c),
  });

  runStreamingJob(jobId, () =>
    helmDisableStream({
      release,
      namespace,
      resource: { kind, name, namespace: parsed.data.resourceNamespace ?? "" },
      source: parsed.data.source,
      valuesYaml: valuesToYAML(parsed.data.values ?? {}),
      deletePvcs: parsed.data.deletePvcs,
      timeoutSeconds: parsed.data.timeoutSeconds,
    })
  );
  return c.json({ jobId, stream: `/charts/jobs/${jobId}/stream`, status: "pending" }, 202);
});

// POST /charts/:release/resources/:kind/:name/enable
chartsRouter.post("/:release/resources/:kind/:name/enable", async (c) => {
  const release = c.req.param("release");
  const kind = c.req.param("kind");
  const name = c.req.param("name");
  const namespace = c.req.query("namespace");
  if (!namespace) return c.json({ error: "namespace query param required" }, 400);

  const body = await c.req.json();
  const parsed = DisableResourceSchema.safeParse(body); // same shape sans deletePvcs use
  if (!parsed.success) return c.json({ error: parsed.error.format() }, 400);

  clearDisabled(release, namespace, kind, name, parsed.data.resourceNamespace ?? "");

  const jobId = createJob(release, namespace, `enable:${kind}/${name}`, initiator(c));
  runStreamingJob(jobId, () =>
    helmEnableStream({
      release,
      namespace,
      resource: { kind, name, namespace: parsed.data.resourceNamespace ?? "" },
      source: parsed.data.source,
      valuesYaml: valuesToYAML(parsed.data.values ?? {}),
      timeoutSeconds: parsed.data.timeoutSeconds,
    })
  );
  return c.json({ jobId, stream: `/charts/jobs/${jobId}/stream`, status: "pending" }, 202);
});

// DELETE /charts/:release/resources/:kind/:name → alias for disable + force required
chartsRouter.delete("/:release/resources/:kind/:name", async (c) => {
  if (!requireForce(c)) return c.json({ error: "Destructive op requires ?force=true" }, 400);
  return chartsRouter.fetch(
    new Request(c.req.url.replace(`/${c.req.param("name")}`, `/${c.req.param("name")}/disable`), {
      method: "POST",
      headers: c.req.header() as any,
      body: c.req.raw.body,
    }) as any,
    c.env
  ) as any;
});

// --- runner ----------------------------------------------------------------

async function runStreamingJob(jobId: string, makeStream: () => AsyncIterable<any>) {
  setJobStatus(jobId, "running");
  let failed = false;
  try {
    for await (const ev of makeStream()) {
      const norm = {
        phase: ev.phase,
        message: ev.message,
        kind: ev.resourceKind,
        name: ev.resourceName,
        namespace: ev.resourceNamespace,
        error: ev.error,
        ts: Number(ev.ts ?? Date.now()),
      };
      appendEvent(jobId, norm);
      if (norm.phase === "error") failed = true;
    }
  } catch (err: any) {
    appendEvent(jobId, { phase: "error", error: err?.message ?? String(err) });
    failed = true;
  }
  setJobStatus(jobId, failed ? "failed" : "succeeded");
}

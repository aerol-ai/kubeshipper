import { Hono } from "hono";
import { stream as honoStream } from "hono/streaming";
import { ServiceSpecSchema, PartialServiceSpecSchema } from "./validation";
import { stateStore } from "../store/db";
import * as k8s from "../kubernetes/adapter";
import { streamLogs } from "../kubernetes/logs";
import { MANAGED_NAMESPACES } from "../kubernetes/adapter";
import { PassThrough } from "stream";

export const servicesRouter = new Hono();

// POST /services: Write desired spec as PENDING
servicesRouter.post("/", async (c) => {
  try {
    const body = await c.req.json();
    const specResult = ServiceSpecSchema.safeParse(body);
    
    if (!specResult.success) {
      return c.json({ error: "Validation failed", details: specResult.error.format() }, 400);
    }

    const id = specResult.data.name;
    const spec = specResult.data;

    // Reject early if the requested namespace is not in the allowed list
    const targetNs = spec.namespace ?? [...MANAGED_NAMESPACES][0];
    if (!MANAGED_NAMESPACES.has(targetNs!)) {
      return c.json({
        error: "Namespace not allowed",
        details: `"${targetNs}" is not in MANAGED_NAMESPACES. Allowed: ${[...MANAGED_NAMESPACES].join(", ")}`,
      }, 400);
    }

    // Asynchronous mode: Save to DB as PENDING
    stateStore.setService(id, spec, "PENDING");
    stateStore.logEvent(id, "Created", "Service deployment requested via API");
    
    return c.json({ message: "Service accepted for deployment", id, status: "PENDING" }, 202);
  } catch (err: any) {
    return c.json({ error: "API request failed", details: err.message }, 500);
  }
});

// GET /services: List all
servicesRouter.get("/", async (c) => {
  const all = stateStore.getAllServices();
  return c.json({ services: all });
});

// GET /services/:id: Retrieve from DB + live K8s status overlay
servicesRouter.get("/:id", async (c) => {
  const id = c.req.param("id");
  const record = stateStore.getService(id);

  if (!record) {
    return c.json({ error: "Service not found in DB" }, 404);
  }

  try {
    const k8sStatus = await k8s.getServiceStatus(id, record.spec.namespace);
    return c.json({ 
      id: record.id, 
      spec: record.spec, 
      status: record.status, 
      created_at: record.created_at,
      updated_at: record.updated_at,
      k8sStatus 
    });
  } catch (err: any) {
    return c.json({ error: "Failed to fetch status overlay", details: err.message }, 500);
  }
});

// PATCH /services/:id: Merge new spec & set PENDING
servicesRouter.patch("/:id", async (c) => {
  const id = c.req.param("id");
  const existing = stateStore.getService(id);

  if (!existing) {
    return c.json({ error: "Service not found" }, 404);
  }

  try {
    const body = await c.req.json();
    const patchResult = PartialServiceSpecSchema.safeParse(body);

    if (!patchResult.success) {
      return c.json({ error: "Validation failed", details: patchResult.error.format() }, 400);
    }

    const mergedSpec = { ...existing.spec, ...patchResult.data };
    
    stateStore.setService(id, mergedSpec, "PENDING");
    stateStore.logEvent(id, "Updated", "Service spec patched via API");

    return c.json({ message: "Update accepted", id, status: "PENDING" }, 202);
  } catch (err: any) {
    return c.json({ error: "Patch failed", details: err.message }, 500);
  }
});

// DELETE /services/:id: Trigger hard delete inline for simplicity (could also be async)
servicesRouter.delete("/:id", async (c) => {
  const id = c.req.param("id");
  
  if (!stateStore.getService(id)) {
    return c.json({ error: "Service not found" }, 404);
  }

  try {
    stateStore.logEvent(id, "Deleting", "Tear down requested via API");
    await k8s.deleteService(id, stateStore.getService(id)!.spec.namespace);
    stateStore.deleteService(id);
    return c.json({ message: "Service deleted", id });
  } catch (err: any) {
    return c.json({ error: "Delete failed", details: err.message }, 500);
  }
});

// POST /services/:id/restart: Restart rollout
servicesRouter.post("/:id/restart", async (c) => {
  const id = c.req.param("id");
  
  if (!stateStore.getService(id)) {
    return c.json({ error: "Service not found" }, 404);
  }

  try {
    stateStore.logEvent(id, "Restarting", "Manual rollout restart requested");
    await k8s.restartService(id, stateStore.getService(id)!.spec.namespace);
    return c.json({ message: "Service rollout restarted", id });
  } catch (err: any) {
    return c.json({ error: "Restart failed", details: err.message }, 500);
  }
});

// GET /services/:id/events: Fetch audit log
servicesRouter.get("/:id/events", async (c) => {
  const id = c.req.param("id");
  
  if (!stateStore.getService(id)) {
    return c.json({ error: "Service not found" }, 404);
  }

  const evts = stateStore.getEvents(id);
  return c.json({ events: evts });
});

// GET /services/:id/logs: Stream logs
servicesRouter.get("/:id/logs", async (c) => {
  const id = c.req.param("id");

  if (!stateStore.getService(id)) {
    return c.json({ error: "Service not found" }, 404);
  }

  return honoStream(c, async (stream) => {
    const record = stateStore.getService(id)!;
    const pt = new PassThrough();
    pt.on("data", (chunk) => stream.write(new Uint8Array(chunk)));
    pt.on("end", () => stream.close());

    // Use the first allowed namespace as fallback if spec has none
    const fallbackNs = [...MANAGED_NAMESPACES][0]!;
    streamLogs(record.spec.namespace ?? fallbackNs, id, pt).catch((err) => {
      pt.write(`\nError streaming logs: ${err.message}\n`);
      pt.end();
    });

    await new Promise((resolve) => pt.on("end", resolve));
  });
});

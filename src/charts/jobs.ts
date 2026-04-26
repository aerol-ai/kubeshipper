import { db } from "../store/db";
import type { JobRow, JobStatus } from "./schema";
import { randomUUID } from "node:crypto";

// In-memory subscriber registry: jobId → set of write callbacks.
// SSE handlers register themselves and receive events as they're appended.
const subscribers = new Map<string, Set<(ev: any) => void>>();

export function createJob(release: string, namespace: string, operation: string, initiator?: string): string {
  const id = randomUUID();
  db.query(
    `INSERT INTO jobs (id, release, namespace, operation, status, started_at, initiator)
     VALUES (?, ?, ?, ?, 'pending', ?, ?)`
  ).run(id, release, namespace, operation, Date.now(), initiator ?? null);
  return id;
}

export function setJobStatus(id: string, status: JobStatus) {
  const ended = status === "succeeded" || status === "failed" ? Date.now() : null;
  db.query(`UPDATE jobs SET status = ?, ended_at = COALESCE(?, ended_at) WHERE id = ?`)
    .run(status, ended, id);

  // Push terminal status to subscribers so SSE handlers can close cleanly.
  if (status === "succeeded" || status === "failed") {
    publish(id, { phase: "complete", status });
  }
}

export function appendEvent(id: string, ev: Record<string, unknown>) {
  // jsonl append; cheap reads are unimportant — the SSE stream is the hot path.
  db.query(`UPDATE jobs SET events_jsonl = events_jsonl || ? || char(10) WHERE id = ?`)
    .run(JSON.stringify(ev), id);
  publish(id, ev);
}

function publish(id: string, ev: any) {
  const subs = subscribers.get(id);
  if (!subs) return;
  for (const cb of subs) {
    try { cb(ev); } catch { /* drop subscriber errors */ }
  }
}

export function subscribe(id: string, cb: (ev: any) => void): () => void {
  let set = subscribers.get(id);
  if (!set) {
    set = new Set();
    subscribers.set(id, set);
  }
  set.add(cb);
  return () => {
    set!.delete(cb);
    if (set!.size === 0) subscribers.delete(id);
  };
}

export function getJob(id: string): JobRow | undefined {
  return db.query<JobRow, [string]>(`SELECT * FROM jobs WHERE id = ?`).get(id) ?? undefined;
}

export function getJobEvents(id: string): any[] {
  const row = getJob(id);
  if (!row) return [];
  if (!row.events_jsonl) return [];
  return row.events_jsonl
    .split("\n")
    .filter((l) => l.length > 0)
    .map((l) => JSON.parse(l));
}

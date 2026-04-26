import { db } from "../store/db";

// Chart-related tables. Live release state stays in Helm (release secrets in-cluster).
// Only jobs, the disabled-resource ledger, and audit log are stored locally.
export function initChartsSchema() {
  db.query(`
    CREATE TABLE IF NOT EXISTS jobs (
      id            TEXT PRIMARY KEY,
      release       TEXT NOT NULL,
      namespace     TEXT NOT NULL,
      operation     TEXT NOT NULL,
      status        TEXT NOT NULL,
      events_jsonl  TEXT NOT NULL DEFAULT '',
      started_at    INTEGER NOT NULL,
      ended_at      INTEGER,
      initiator     TEXT
    );
  `).run();

  db.query(`
    CREATE INDEX IF NOT EXISTS jobs_release_idx ON jobs(release, namespace);
  `).run();

  db.query(`
    CREATE TABLE IF NOT EXISTS disabled_resources (
      release      TEXT NOT NULL,
      namespace    TEXT NOT NULL,
      kind         TEXT NOT NULL,
      name         TEXT NOT NULL,
      resource_ns  TEXT NOT NULL DEFAULT '',
      disabled_at  INTEGER NOT NULL,
      PRIMARY KEY (release, namespace, kind, name, resource_ns)
    );
  `).run();

  db.query(`
    CREATE TABLE IF NOT EXISTS chart_audit (
      id            INTEGER PRIMARY KEY AUTOINCREMENT,
      ts            INTEGER NOT NULL,
      initiator     TEXT,
      operation     TEXT NOT NULL,
      release       TEXT NOT NULL,
      namespace     TEXT NOT NULL,
      payload_hash  TEXT,
      outcome       TEXT
    );
  `).run();
}

export type JobStatus = "pending" | "running" | "succeeded" | "failed" | "drift_detected";

export interface JobRow {
  id: string;
  release: string;
  namespace: string;
  operation: string;
  status: JobStatus;
  events_jsonl: string;
  started_at: number;
  ended_at: number | null;
  initiator: string | null;
}

export interface DisabledResourceRow {
  release: string;
  namespace: string;
  kind: string;
  name: string;
  resource_ns: string;
  disabled_at: number;
}

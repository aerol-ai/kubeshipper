import { db } from "../store/db";
import type { DisabledResourceRow } from "./schema";

export function recordDisabled(
  release: string,
  namespace: string,
  kind: string,
  name: string,
  resourceNs = ""
) {
  db.query(
    `INSERT OR IGNORE INTO disabled_resources
     (release, namespace, kind, name, resource_ns, disabled_at)
     VALUES (?, ?, ?, ?, ?, ?)`
  ).run(release, namespace, kind, name, resourceNs, Date.now());
}

export function clearDisabled(
  release: string,
  namespace: string,
  kind: string,
  name: string,
  resourceNs = ""
) {
  db.query(
    `DELETE FROM disabled_resources
     WHERE release = ? AND namespace = ? AND kind = ? AND name = ? AND resource_ns = ?`
  ).run(release, namespace, kind, name, resourceNs);
}

export function listDisabled(release: string, namespace: string): DisabledResourceRow[] {
  return db
    .query<DisabledResourceRow, [string, string]>(
      `SELECT * FROM disabled_resources WHERE release = ? AND namespace = ? ORDER BY disabled_at`
    )
    .all(release, namespace);
}

import { db } from "../store/db";
import { createHash } from "node:crypto";

// Redact known sensitive fields before hashing the request body for audit.
const SENSITIVE_KEYS = new Set([
  "password",
  "token",
  "sshKeyPem",
  "tgzBase64",
  "stringData",
  "auth",
]);

function redact(obj: unknown): unknown {
  if (obj === null || typeof obj !== "object") return obj;
  if (Array.isArray(obj)) return obj.map(redact);
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(obj)) {
    if (SENSITIVE_KEYS.has(k)) {
      out[k] = "[redacted]";
    } else {
      out[k] = redact(v);
    }
  }
  return out;
}

export function auditLog(args: {
  initiator?: string;
  operation: string;
  release: string;
  namespace: string;
  payload?: unknown;
  outcome: "accepted" | "rejected" | "error";
}) {
  const redacted = redact(args.payload ?? {});
  const hash = createHash("sha256").update(JSON.stringify(redacted)).digest("hex");
  db.query(
    `INSERT INTO chart_audit (ts, initiator, operation, release, namespace, payload_hash, outcome)
     VALUES (?, ?, ?, ?, ?, ?, ?)`
  ).run(
    Date.now(),
    args.initiator ?? null,
    args.operation,
    args.release,
    args.namespace,
    hash,
    args.outcome
  );
}

import { Database } from "bun:sqlite";
import type { ServiceSpec } from "../api/validation";

// DB_PATH can point to a mounted PVC path in Kubernetes.
// IMPORTANT: kubeshipper must run as a single replica when using SQLite.
// SQLite does not support concurrent multi-process writes; multiple pods
// would produce split-brain state and double-process every deployment.
export const db = new Database(process.env.DB_PATH ?? "kubeshipper.sqlite", { create: true });

// Enable Write-Ahead Logging for better concurrency and performance
db.query("PRAGMA journal_mode = WAL;").run();

// Schema Initialization
export function initDB() {
  db.query(`
    CREATE TABLE IF NOT EXISTS services (
      id TEXT PRIMARY KEY,
      spec_json TEXT NOT NULL,
      status TEXT NOT NULL, -- PENDING, DEPLOYING, READY, FAILED
      last_ready_spec_json TEXT,
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
      updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );
  `).run();

  db.query(`
    CREATE TABLE IF NOT EXISTS events (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      service_id TEXT NOT NULL,
      type TEXT NOT NULL,
      message TEXT NOT NULL,
      timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
      FOREIGN KEY(service_id) REFERENCES services(id) ON DELETE CASCADE
    );
  `).run();
}

/**
 * State Models Interface mapping DB rows to TypeScript types.
 */
export interface ServiceRow {
  id: string;
  spec_json: string;
  status: "PENDING" | "DEPLOYING" | "READY" | "FAILED";
  last_ready_spec_json?: string | null;
  created_at: string;
  updated_at: string;
}

export interface EventRow {
  id: number;
  service_id: string;
  type: string;
  message: string;
  timestamp: string;
}

// Data Access Object (DAO) wrappers

export const stateStore = {
  getService(id: string): (ServiceRow & { spec: ServiceSpec }) | undefined {
    const row = db.query<ServiceRow, [string]>("SELECT * FROM services WHERE id = ?").get(id);
    if (!row) return undefined;
    return { ...row, spec: JSON.parse(row.spec_json) as ServiceSpec };
  },

  getAllServices(): (ServiceRow & { spec: ServiceSpec })[] {
    const rows = db.query<ServiceRow, []>("SELECT * FROM services").all();
    return rows.map((r: ServiceRow) => ({ ...r, spec: JSON.parse(r.spec_json) as ServiceSpec }));
  },

  setService(id: string, spec: ServiceSpec, status: ServiceRow["status"] = "PENDING"): void {
    const existing = db.query("SELECT id FROM services WHERE id = ?").get(id);
    const ts = new Date().toISOString();
    
    if (existing) {
      db.query(`
        UPDATE services 
        SET spec_json = ?, status = ?, updated_at = ? 
        WHERE id = ?
      `).run(JSON.stringify(spec), status, ts, id);
    } else {
      db.query(`
        INSERT INTO services (id, spec_json, status, created_at, updated_at) 
        VALUES (?, ?, ?, ?, ?)
      `).run(id, JSON.stringify(spec), status, ts, ts);
    }
  },

  markReady(id: string, spec: ServiceSpec): void {
    const ts = new Date().toISOString();
    db.query(`
      UPDATE services 
      SET status = 'READY', last_ready_spec_json = ?, updated_at = ? 
      WHERE id = ?
    `).run(JSON.stringify(spec), ts, id);
  },

  updateStatus(id: string, status: ServiceRow["status"]): void {
    const ts = new Date().toISOString();
    db.query(`UPDATE services SET status = ?, updated_at = ? WHERE id = ?`).run(status, ts, id);
  },

  deleteService(id: string): void {
    db.query("DELETE FROM services WHERE id = ?").run(id);
  },

  // Events DAO
  logEvent(serviceId: string, type: string, message: string): void {
    const ts = new Date().toISOString();
    db.query(`
      INSERT INTO events (service_id, type, message, timestamp) 
      VALUES (?, ?, ?, ?)
    `).run(serviceId, type, message, ts);
  },

  getEvents(serviceId: string): EventRow[] {
    return db.query<EventRow, [string]>("SELECT * FROM events WHERE service_id = ? ORDER BY timestamp DESC").all(serviceId);
  }
};

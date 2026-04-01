import { Hono } from "hono";
import { logger } from "hono/logger";
import { servicesRouter } from "./api/routes";
import { initDB } from "./store/db";
import { startBackgroundWorkers } from "./worker";

// Initialize database schema
initDB();

// Start K8s orchestration loops
startBackgroundWorkers();

const app = new Hono();

app.use("*", logger());

app.get("/health", (c) => c.json({ status: "ok" }));

app.route("/services", servicesRouter);

export default {
  port: process.env.PORT || 3000,
  fetch: app.fetch,
};

console.log(`KubeShipper API is running on http://localhost:${process.env.PORT || 3000}`);

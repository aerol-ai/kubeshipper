import { Hono } from "hono";
import { logger } from "hono/logger";
import { servicesRouter } from "./api/routes";
import { authMiddleware } from "./api/auth";
import { initDB } from "./store/db";
import { startBackgroundWorkers } from "./worker";

// Initialize database schema
initDB();

// Start K8s orchestration loops
startBackgroundWorkers();

const app = new Hono();

app.use("*", logger());

// /health is always public — used by liveness/readiness probes
app.get("/health", (c) => c.json({ status: "ok" }));

// All /services routes require auth when AUTH_TOKEN is set in env
app.use("/services/*", authMiddleware);
app.use("/services", authMiddleware);

app.route("/services", servicesRouter);

export default {
  port: process.env.PORT || 3000,
  fetch: app.fetch,
};

console.log(`KubeShipper API is running on http://localhost:${process.env.PORT || 3000}`);

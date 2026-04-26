import { Hono } from "hono";
import { logger } from "hono/logger";
import { servicesRouter } from "./api/routes";
import { chartsRouter } from "./charts/routes";
import { authMiddleware } from "./api/auth";
import { initDB } from "./store/db";
import { initChartsSchema } from "./charts/schema";
import { startBackgroundWorkers } from "./worker";

// Initialize database schema
initDB();
initChartsSchema();

// Start K8s orchestration loops
startBackgroundWorkers();

const app = new Hono();
const startedAt = new Date().toISOString();

app.use("*", logger());

// Root endpoint — confirms the server is reachable
app.get("/", (c) =>
  c.json({
    name: "kubeshipper",
    description: "Lightweight Kubernetes deployment + Helm chart API",
    docs: { services: "/services", charts: "/charts" },
  })
);

// /health is always public — used by liveness/readiness probes
app.get("/health", (c) =>
  c.json({
    status: "ok",
    uptime: Math.floor(process.uptime()),
    startedAt,
    version: process.env.APP_VERSION ?? "unknown",
  })
);

// All non-health routes require auth when AUTH_TOKEN is set
app.use("/services/*", authMiddleware);
app.use("/services", authMiddleware);
app.use("/charts/*", authMiddleware);
app.use("/charts", authMiddleware);

app.route("/services", servicesRouter);
app.route("/charts", chartsRouter);

export default {
  port: process.env.PORT || 3000,
  fetch: app.fetch,
};

console.log(`KubeShipper API is running on http://localhost:${process.env.PORT || 3000}`);

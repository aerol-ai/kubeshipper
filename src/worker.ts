import { stateStore, db } from "./store/db";
import * as k8s from "./kubernetes/adapter";

let pollingInterval: ReturnType<typeof setInterval>;

export function startBackgroundWorkers() {
  console.log("Starting KubeShipper worker loops...");

  // If the process crashed mid-deploy, services may be stuck in DEPLOYING.
  // Re-queue them so the next loop picks them up cleanly.
  db.query("UPDATE services SET status = 'PENDING' WHERE status = 'DEPLOYING'").run();

  // High frequency loop for orchestration (every 5 seconds)
  pollingInterval = setInterval(async () => {
    try {
      await processPendingDeployments();
      await watchDeployingRollouts();
      await reconcileDrift();
    } catch (err) {
      console.error("Worker loop iteration error:", err);
    }
  }, 5000);
}

// 1. Process new requests (PENDING -> DEPLOYING)
async function processPendingDeployments() {
  const pendingRows = db.query("SELECT * FROM services WHERE status = 'PENDING'").all() as any[];
  
  for (const row of pendingRows) {
    const spec = JSON.parse(row.spec_json);
    try {
      stateStore.updateStatus(row.id, "DEPLOYING");
      stateStore.logEvent(row.id, "Deploying", "Worker picked up deployment task and started SSA");
      
      await k8s.deployService(spec);
    } catch (err: any) {
      console.error(`Failed to execute SSA for ${row.id}:`, err);
      stateStore.updateStatus(row.id, "FAILED");
      stateStore.logEvent(row.id, "DeployFailed", `Server Side Apply Failed: ${err.message}`);
    }
  }
}

// 2. Watch active rollouts (DEPLOYING -> READY or FAILED/ROLLBACK)
async function watchDeployingRollouts() {
  const deployingRows = db.query("SELECT * FROM services WHERE status = 'DEPLOYING'").all() as any[];

  for (const row of deployingRows) {
    try {
      const status = await k8s.getServiceStatus(row.id);
      
      if (status.ready) {
        stateStore.markReady(row.id, JSON.parse(row.spec_json));
        stateStore.logEvent(row.id, "RolloutComplete", "Deployment rollout successfully finished and is Ready");
        continue;
      }

      // Rollout Intelligence: Detect Progressive failures
      // Check for ProgressDeadlineExceeded (ReplicaFailure etc)
      let isFailed = false;
      let failReason = "";

      for (const cond of status.conditions || []) {
        if (cond.type === "Progressing" && cond.status === "False" && cond.reason === "ProgressDeadlineExceeded") {
          isFailed = true;
          failReason = cond.message || "ProgressDeadlineExceeded";
        }
        if (cond.type === "ReplicaFailure" && cond.status === "True") {
          isFailed = true;
          failReason = cond.message || "ReplicaFailure";
        }
      }

      if (isFailed) {
        stateStore.logEvent(row.id, "RolloutFailed", `Deployment failed health checks: ${failReason}`);
        
        if (row.last_ready_spec_json) {
           stateStore.logEvent(row.id, "AutoRollback", "Initiating automatic rollback to previous known READY spec");
           
           // Restore previous healthy spec
           const safeSpec = JSON.parse(row.last_ready_spec_json);
           
           // Overwrite the DB spec
           stateStore.setService(row.id, safeSpec, "PENDING"); // Sends it back to worker pipeline
        } else {
           stateStore.updateStatus(row.id, "FAILED");
           stateStore.logEvent(row.id, "RollbackWarning", "Rollout failed natively, but no previous states existed to rollback to.");
        }
      }

    } catch (err: any) {
      console.error(`Status check failed for ${row.id}:`, err);
    }
  }
}

// 3. Drift Reconciliation (READY vs Actual K8s)
let lastReconcile = Date.now();
async function reconcileDrift() {
  const now = Date.now();
  // Run drift check only every 60 seconds to save API calls
  if (now - lastReconcile < 60000) return;
  lastReconcile = now;

  const readyRows = db.query("SELECT * FROM services WHERE status = 'READY'").all() as any[];

  for (const row of readyRows) {
    try {
      const spec = JSON.parse(row.spec_json);
      const status = await k8s.getServiceStatus(row.id, spec.namespace);

      // Simple drift check: if Deployment is completely missing from K8s but DB says READY
      if (!status.ready && status.reason === "Deployment not found") {
        stateStore.logEvent(row.id, "DriftDetected", "Service marked READY but missing in Kubernetes. Forcing Re-apply.");
        stateStore.updateStatus(row.id, "PENDING"); // Sends it back into the deployment loop
      }
      
      // We rely natively on SSA for drift. You could also just naively re-run k8s.deployService(spec) 
      // here every hour to let Kubernetes API server correct any manual edits.
    } catch (err: any) {
       console.error(`Reconciliation failed for ${row.id}:`, err);
    }
  }
}

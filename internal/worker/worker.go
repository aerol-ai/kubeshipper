package worker

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/chartmonitor"
	"github.com/aerol-ai/kubeshipper/internal/kube"
	"github.com/aerol-ai/kubeshipper/internal/rollout"
	"github.com/aerol-ai/kubeshipper/internal/store"
)

// Worker drives the lifecycle of `services` rows:
//
//	PENDING  ── deploy via SSA ──>  DEPLOYING
//	DEPLOYING ── readiness OK ─>  READY
//	DEPLOYING ── progress fail ─>  attempt rollback to last_ready_spec_json
//
// Plus a slower drift loop that re-pends services whose Deployment is missing.
//
// Every operator-initiated mutation creates a job and attaches it to the row
// (services.job_id). The worker publishes typed events to that job's SSE
// pubsub on every state transition. Drift-triggered re-pends have no JobID
// and produce no events — they're internal reconciliation, not user-driven.
type Worker struct {
	store        *store.Store
	kube         *kube.Client
	rollouts     *rollout.Manager
	chartMonitor *chartmonitor.Manager
}

func New(s *store.Store, kc *kube.Client, rollouts *rollout.Manager, chartMonitor *chartmonitor.Manager) *Worker {
	return &Worker{store: s, kube: kc, rollouts: rollouts, chartMonitor: chartMonitor}
}

func (w *Worker) Run(ctx context.Context) {
	// On boot, recover anything stuck in DEPLOYING — likely a process crash mid-deploy.
	if err := w.store.ResetStuckDeployments(); err != nil {
		log.Printf("worker: reset stuck deployments: %v", err)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	driftTicker := time.NewTicker(60 * time.Second)
	defer driftTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processPending(ctx)
			w.watchDeploying(ctx)
		case <-driftTicker.C:
			w.reconcileDrift(ctx)
			if w.rollouts != nil {
				w.rollouts.SyncAll(ctx)
			}
			if w.chartMonitor != nil {
				w.chartMonitor.SyncAll(ctx)
			}
		}
	}
}

// emit publishes a typed Event to the job's SSE pubsub when one is attached.
// Phase mapping:
//
//	"Deploying"|"AutoRollback"               → phase: "apply"
//	"RolloutComplete"                        → phase: "done"   (terminal: succeeded)
//	"DeployFailed"|"RollbackWarning"         → phase: "error"  (terminal: failed)
//	"RolloutFailed"                          → phase: "wait"   (rollback may follow)
//	"DriftDetected"                          → phase: "validation"
//
// Terminal phases flip the linked job's status (succeeded/failed) and detach
// the job from the service, closing the SSE stream for live subscribers.
//
// Rows with no JobID (e.g. drift-triggered re-pends) are silently skipped —
// the next operator-initiated PATCH/POST will start a fresh job.
func (w *Worker) emit(svc *store.Service, eventType, message string) {
	if svc.JobID == "" {
		return
	}

	phase := "validation"
	terminal := store.JobStatus("")
	switch eventType {
	case "Deploying", "AutoRollback":
		phase = "apply"
	case "RolloutComplete":
		phase = "done"
		terminal = store.JobSucceeded
	case "DeployFailed", "RollbackWarning":
		// Hard failures with no recovery path → terminal.
		phase = "error"
		terminal = store.JobFailed
	case "RolloutFailed":
		// Soft failure: the watcher may auto-rollback. Emit as "wait" so the
		// stream stays open; either AutoRollback or RollbackWarning closes it.
		phase = "wait"
	}

	ev := store.Event{Phase: phase, Message: eventType + ": " + message, TS: time.Now().UnixMilli()}
	if phase == "error" {
		ev.Error = message
	}
	_ = w.store.AppendEvent(svc.JobID, ev)

	if terminal != "" {
		_ = w.store.SetJobStatus(svc.JobID, terminal)
		// Detach so the next PATCH gets a fresh job.
		_ = w.store.AttachJob(svc.ID, "")
	}
}

func (w *Worker) processPending(ctx context.Context) {
	rows, err := w.store.ServicesByStatus(store.StatusPending)
	if err != nil {
		log.Printf("worker: list pending: %v", err)
		return
	}
	for _, row := range rows {
		var spec kube.ServiceSpec
		if err := json.Unmarshal(row.Spec, &spec); err != nil {
			_ = w.store.UpdateStatus(row.ID, store.StatusFailed)
			w.emit(row, "DeployFailed", "invalid spec_json: "+err.Error())
			continue
		}
		_ = w.store.UpdateStatus(row.ID, store.StatusDeploying)
		w.emit(row, "Deploying", "Worker picked up deployment task and started SSA")

		if err := w.kube.DeployService(ctx, &spec); err != nil {
			log.Printf("worker: deploy %s: %v", row.ID, err)
			_ = w.store.UpdateStatus(row.ID, store.StatusFailed)
			w.emit(row, "DeployFailed", "Server-side apply failed: "+err.Error())
		}
	}
}

func (w *Worker) watchDeploying(ctx context.Context) {
	rows, err := w.store.ServicesByStatus(store.StatusDeploying)
	if err != nil {
		return
	}
	for _, row := range rows {
		var spec kube.ServiceSpec
		_ = json.Unmarshal(row.Spec, &spec)

		status, err := w.kube.ServiceStatus(ctx, row.ID, spec.Namespace)
		if err != nil {
			log.Printf("worker: status %s: %v", row.ID, err)
			continue
		}

		if status.Ready {
			_ = w.store.MarkReady(row.ID, row.Spec)
			w.emit(row, "RolloutComplete", "Deployment rollout successfully finished and is Ready")
			continue
		}

		// Did the rollout permanently fail?
		failReason := ""
		for _, c := range status.Conditions {
			if c.Type == "Progressing" && string(c.Status) == "False" && c.Reason == "ProgressDeadlineExceeded" {
				failReason = c.Message
				if failReason == "" {
					failReason = "ProgressDeadlineExceeded"
				}
				break
			}
			if c.Type == "ReplicaFailure" && string(c.Status) == "True" {
				failReason = c.Message
				if failReason == "" {
					failReason = "ReplicaFailure"
				}
				break
			}
		}
		if failReason == "" {
			continue
		}

		w.emit(row, "RolloutFailed", "Deployment failed health checks: "+failReason)

		if row.LastReadySpec != nil {
			w.emit(row, "AutoRollback", "Reverting to previous READY spec")
			_ = w.store.UpsertService(row.ID, row.LastReadySpec, store.StatusPending)
			// Re-attach the job to the new PENDING incarnation so the rollback rollout streams too.
			if row.JobID != "" {
				_ = w.store.AttachJob(row.ID, row.JobID)
			}
		} else {
			_ = w.store.UpdateStatus(row.ID, store.StatusFailed)
			w.emit(row, "RollbackWarning", "Rollout failed; no previous READY spec to revert to.")
		}
	}
}

func (w *Worker) reconcileDrift(ctx context.Context) {
	rows, err := w.store.ServicesByStatus(store.StatusReady)
	if err != nil {
		return
	}
	for _, row := range rows {
		var spec kube.ServiceSpec
		_ = json.Unmarshal(row.Spec, &spec)
		status, err := w.kube.ServiceStatus(ctx, row.ID, spec.Namespace)
		if err != nil {
			continue
		}
		if !status.Ready && status.Reason == "Deployment not found" {
			// Drift: Deployment was deleted out-of-band. Re-pend the row; the
			// worker's processPending tick will re-apply the stored spec via SSA.
			// No SSE events are emitted because drift handling has no client
			// stream attached.
			_ = w.store.UpdateStatus(row.ID, store.StatusPending)
		}
	}
}

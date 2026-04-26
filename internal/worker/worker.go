package worker

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/kube"
	"github.com/aerol-ai/kubeshipper/internal/store"
)

// Worker drives the lifecycle of `services` rows:
//
//   PENDING  ── deploy via SSA ──>  DEPLOYING
//   DEPLOYING ── readiness OK ─>  READY
//   DEPLOYING ── progress fail ─>  attempt rollback to last_ready_spec_json
//
// Plus a slower drift loop that re-pends services whose Deployment is missing.
type Worker struct {
	store *store.Store
	kube  *kube.Client
}

func New(s *store.Store, kc *kube.Client) *Worker {
	return &Worker{store: s, kube: kc}
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
		}
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
			_ = w.store.LogEvent(row.ID, "DeployFailed", "invalid spec_json: "+err.Error())
			continue
		}
		_ = w.store.UpdateStatus(row.ID, store.StatusDeploying)
		_ = w.store.LogEvent(row.ID, "Deploying", "Worker picked up deployment task and started SSA")

		if err := w.kube.DeployService(ctx, &spec); err != nil {
			log.Printf("worker: deploy %s: %v", row.ID, err)
			_ = w.store.UpdateStatus(row.ID, store.StatusFailed)
			_ = w.store.LogEvent(row.ID, "DeployFailed", "Server-side apply failed: "+err.Error())
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
			_ = w.store.LogEvent(row.ID, "RolloutComplete", "Deployment rollout successfully finished and is Ready")
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

		_ = w.store.LogEvent(row.ID, "RolloutFailed", "Deployment failed health checks: "+failReason)

		if row.LastReadySpec != nil {
			_ = w.store.LogEvent(row.ID, "AutoRollback", "Reverting to previous READY spec")
			_ = w.store.UpsertService(row.ID, row.LastReadySpec, store.StatusPending)
		} else {
			_ = w.store.UpdateStatus(row.ID, store.StatusFailed)
			_ = w.store.LogEvent(row.ID, "RollbackWarning", "Rollout failed; no previous READY spec to revert to.")
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
			_ = w.store.LogEvent(row.ID, "DriftDetected",
				"Service marked READY but missing in Kubernetes. Forcing re-apply.")
			_ = w.store.UpdateStatus(row.ID, store.StatusPending)
		}
	}
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/aerol-ai/kubeshipper/internal/helm"
	"github.com/aerol-ai/kubeshipper/internal/store"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountCharts(r chi.Router) {
	r.Route("/charts", func(g chi.Router) {
		g.Post("/", s.installChart)
		g.Get("/", s.listCharts)
		g.Post("/preflight", s.preflightChart)

		g.Get("/jobs/{id}", s.getJob)
		g.Get("/jobs/{id}/stream", s.streamJob)

		g.Put("/{release}/source", s.saveChartSource)
		g.Get("/{release}", s.getRelease)
		g.Patch("/{release}", s.upgradeRelease)
		g.Delete("/{release}", s.uninstallRelease)
		g.Post("/{release}/rollback", s.rollbackRelease)
		g.Post("/{release}/monitor/check", s.checkChartMonitor)
		g.Post("/{release}/monitor/sync", s.syncChartMonitor)
		g.Post("/{release}/monitor/enable", s.enableChartMonitor)
		g.Post("/{release}/monitor/disable", s.disableChartMonitor)
		g.Get("/{release}/history", s.releaseHistory)
		g.Get("/{release}/diff", s.releaseDiff)
		g.Get("/{release}/values", s.releaseValues)
		g.Get("/{release}/manifest", s.releaseManifest)

		g.Post("/{release}/resources/{kind}/{name}/disable", s.disableResource)
		g.Post("/{release}/resources/{kind}/{name}/enable", s.enableResource)
		g.Delete("/{release}/resources/{kind}/{name}", s.disableResource) // alias
	})
}

// --- helpers ---

func mustQuery(r *http.Request, key string) (string, bool) {
	v := r.URL.Query().Get(key)
	return v, v != ""
}

func requireForce(r *http.Request) bool {
	return r.URL.Query().Get("force") == "true"
}

func decodeBody[T any](r *http.Request, dst *T) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dst)
}

// --- /charts (install) ---

func (s *Server) installChart(w http.ResponseWriter, r *http.Request) {
	var req helm.InstallReq
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON", "details": err.Error()})
		return
	}
	if err := validateInstall(&req); err != nil {
		_ = s.deps.Store.AuditLog(initiator(r), "install", req.Release, req.Namespace, "rejected", req)
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if req.RolloutWatch != nil && s.deps.Rollouts == nil {
		_ = s.deps.Store.AuditLog(initiator(r), "install", req.Release, req.Namespace, "rejected", req)
		writeJSON(w, 503, map[string]string{"error": "rollout watch manager is unavailable"})
		return
	}

	jobID, err := s.deps.Store.CreateJob(req.Release, req.Namespace, "install", initiator(r))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.deps.Store.AuditLog(initiator(r), "install", req.Release, req.Namespace, "accepted", req)

	go s.runJob(jobID, "install", func(ctx context.Context, emit helm.EmitFn) error {
		if err := s.deps.Helm.Install(ctx, &req, emit); err != nil {
			return err
		}
		if _, err := s.persistChartReleaseSource(req.Release, req.Namespace, req.Source); err != nil {
			return fmt.Errorf("helm operation succeeded but chart source persistence failed: %w", err)
		}
		return s.registerChartRolloutWatch(ctx, emit, req.Namespace, req.RolloutWatch)
	})

	writeJSON(w, 202, map[string]string{
		"jobId":     jobID,
		"release":   req.Release,
		"namespace": req.Namespace,
		"stream":    "/api/charts/jobs/" + jobID + "/stream",
		"status":    "pending",
	})
}

func validateInstall(req *helm.InstallReq) error {
	if req.Release == "" || req.Namespace == "" {
		return errors.New("release and namespace required")
	}
	if req.Source == nil || req.Source.Type == "" {
		return errors.New("source.type required")
	}
	switch req.Source.Type {
	case "oci":
		if req.Source.URL == "" || req.Source.Version == "" {
			return errors.New("oci source requires url and version")
		}
	case "https":
		if req.Source.RepoURL == "" || req.Source.Chart == "" {
			return errors.New("https source requires repoUrl and chart")
		}
	case "git":
		if req.Source.RepoURL == "" {
			return errors.New("git source requires repoUrl")
		}
	case "tgz":
		if req.Source.TgzB64 == "" {
			return errors.New("tgz source requires tgzBase64")
		}
	default:
		return fmt.Errorf("unknown source type %q", req.Source.Type)
	}
	return validateRolloutWatch(req.RolloutWatch)
}

// --- /charts (list) ---

func (s *Server) listCharts(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	all := r.URL.Query().Get("all") == "true"
	rels, err := s.deps.Helm.List(r.Context(), ns, all)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"releases": rels})
}

// --- preflight ---

func (s *Server) preflightChart(w http.ResponseWriter, r *http.Request) {
	var req helm.PreflightReq
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.deps.Helm.Preflight(r.Context(), &req)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, out)
}

// --- /charts/{release} ---

func (s *Server) getRelease(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "namespace query param required"})
		return
	}
	out, err := s.deps.Helm.Get(r.Context(), release, ns)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err := s.hydrateChartReleaseResponse(out, release, ns); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) upgradeRelease(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "namespace query param required"})
		return
	}
	var req helm.UpgradeReq
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if req.Source == nil {
		writeJSON(w, 400, map[string]string{"error": "source required"})
		return
	}
	if err := validateRolloutWatch(req.RolloutWatch); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if req.RolloutWatch != nil && s.deps.Rollouts == nil {
		writeJSON(w, 503, map[string]string{"error": "rollout watch manager is unavailable"})
		return
	}

	// Drift gate: try a diff and auto-resync once.
	if diff, err := s.deps.Helm.Diff(r.Context(), release, ns); err == nil && diff.Drifted {
		resyncJobID, _ := s.deps.Store.CreateJob(release, ns, "drift-resync", initiator(r))
		_ = s.deps.Store.AppendEvent(resyncJobID, store.Event{
			Phase: "validation", Message: "drift detected; auto-resyncing",
		})
		go s.runJob(resyncJobID, "drift-resync", func(ctx context.Context, emit helm.EmitFn) error {
			r := &helm.UpgradeReq{
				Source: req.Source, Values: req.Values,
				Atomic: ptrTrue(), Wait: ptrTrue(),
				ReuseValues: true, TimeoutSeconds: 300,
			}
			return s.deps.Helm.Upgrade(ctx, release, ns, r, emit)
		})
	}

	jobID, err := s.deps.Store.CreateJob(release, ns, "upgrade", initiator(r))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.deps.Store.AuditLog(initiator(r), "upgrade", release, ns, "accepted", req)

	go s.runJob(jobID, "upgrade", func(ctx context.Context, emit helm.EmitFn) error {
		if err := s.deps.Helm.Upgrade(ctx, release, ns, &req, emit); err != nil {
			return err
		}
		if _, err := s.persistChartReleaseSource(release, ns, req.Source); err != nil {
			return fmt.Errorf("helm operation succeeded but chart source persistence failed: %w", err)
		}
		return s.registerChartRolloutWatch(ctx, emit, ns, req.RolloutWatch)
	})
	writeJSON(w, 202, map[string]string{
		"jobId":  jobID,
		"stream": "/api/charts/jobs/" + jobID + "/stream",
		"status": "pending",
	})
}

func (s *Server) uninstallRelease(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "namespace query param required"})
		return
	}
	if !requireForce(r) {
		writeJSON(w, 400, map[string]string{"error": "Destructive op requires ?force=true"})
		return
	}
	out, err := s.deps.Helm.Uninstall(r.Context(), release, ns, true, 300)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.deps.Store.DeleteChartReleaseConfig(release, ns)
	_ = s.deps.Store.AuditLog(initiator(r), "uninstall", release, ns, "accepted", nil)
	writeJSON(w, 200, out)
}

func (s *Server) rollbackRelease(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "namespace query param required"})
		return
	}
	var req helm.RollbackReq
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	rev, err := s.deps.Helm.Rollback(r.Context(), release, ns, req.Revision, req.Wait, req.TimeoutSeconds)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.syncStoredChartReleaseVersion(r.Context(), release, ns)
	_ = s.deps.Store.AuditLog(initiator(r), "rollback", release, ns, "accepted", req)
	writeJSON(w, 200, map[string]any{"ok": true, "new_revision": rev})
}

func (s *Server) releaseHistory(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "namespace query param required"})
		return
	}
	out, err := s.deps.Helm.History(r.Context(), release, ns, 0)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"entries": out})
}

func (s *Server) releaseDiff(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "namespace query param required"})
		return
	}
	out, err := s.deps.Helm.Diff(r.Context(), release, ns)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) releaseValues(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "namespace query param required"})
		return
	}
	out, err := s.deps.Helm.Get(r.Context(), release, ns)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"values_yaml": out.ValuesYAML})
}

func (s *Server) releaseManifest(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "namespace query param required"})
		return
	}
	out, err := s.deps.Helm.Get(r.Context(), release, ns)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write([]byte(out.Manifest))
}

// --- disable / enable resource ---

func (s *Server) disableResource(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	kind := chi.URLParam(r, "kind")
	name := chi.URLParam(r, "name")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "namespace query param required"})
		return
	}
	if !requireForce(r) {
		writeJSON(w, 400, map[string]string{"error": "Destructive op requires ?force=true"})
		return
	}

	var req helm.DisableReq
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	jobID, err := s.deps.Store.CreateJob(release, ns, fmt.Sprintf("disable:%s/%s", kind, name), initiator(r))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.deps.Store.AuditLog(initiator(r), "disable-resource", release, ns, "accepted", map[string]any{"kind": kind, "name": name})

	go s.runJob(jobID, "disable", func(ctx context.Context, emit helm.EmitFn) error {
		return s.deps.Helm.DisableResource(ctx, release, ns, kind, name, req.ResourceNamespace,
			req.Source, req.Values, req.DeletePvcs, req.TimeoutSeconds, emit)
	})
	writeJSON(w, 202, map[string]string{"jobId": jobID, "stream": "/api/charts/jobs/" + jobID + "/stream", "status": "pending"})
}

func (s *Server) enableResource(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	kind := chi.URLParam(r, "kind")
	name := chi.URLParam(r, "name")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "namespace query param required"})
		return
	}
	var req helm.DisableReq
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	jobID, err := s.deps.Store.CreateJob(release, ns, fmt.Sprintf("enable:%s/%s", kind, name), initiator(r))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	go s.runJob(jobID, "enable", func(ctx context.Context, emit helm.EmitFn) error {
		return s.deps.Helm.EnableResource(ctx, release, ns, kind, name, req.ResourceNamespace,
			req.Source, req.Values, req.TimeoutSeconds, emit)
	})
	writeJSON(w, 202, map[string]string{"jobId": jobID, "stream": "/api/charts/jobs/" + jobID + "/stream", "status": "pending"})
}

// --- jobs ---

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	j, err := s.deps.Store.GetJob(id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if j == nil {
		writeJSON(w, 404, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, 200, j)
}

func (s *Server) streamJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	j, _ := s.deps.Store.GetJob(id)
	if j == nil {
		writeJSON(w, 404, map[string]string{"error": "job not found"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming unsupported"})
		return
	}

	for _, ev := range j.Events {
		writeSSE(w, ev)
		flusher.Flush()
	}
	if j.Status == store.JobSucceeded || j.Status == store.JobFailed {
		fmt.Fprintf(w, "event: end\ndata: {\"status\":%q}\n\n", string(j.Status))
		flusher.Flush()
		return
	}

	ch, cancel := s.deps.Store.Subscribe(id)
	defer cancel()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, alive := <-ch:
			if !alive {
				return
			}
			writeSSE(w, ev)
			flusher.Flush()
			if ev.Phase == "complete" || ev.Phase == "error" {
				return
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, ev store.Event) {
	body, _ := json.Marshal(ev)
	fmt.Fprintf(w, "data: %s\n\n", body)
}

// --- runner ---

// runJob is the bridge between an HTTP request and a long-running Helm op.
// It supplies an EmitFn that pumps progress events into the job's events_jsonl
// AND fans them out to live SSE subscribers, then sets terminal status when done.
func (s *Server) runJob(jobID, op string, fn func(ctx context.Context, emit helm.EmitFn) error) {
	_ = s.deps.Store.SetJobStatus(jobID, store.JobRunning)

	var failedOnce sync.Once
	var failed bool
	emit := func(ev store.Event) {
		_ = s.deps.Store.AppendEvent(jobID, ev)
		if ev.Phase == "error" {
			failedOnce.Do(func() { failed = true })
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fn(ctx, emit); err != nil && !failed {
		_ = s.deps.Store.AppendEvent(jobID, store.Event{Phase: "error", Error: err.Error()})
		failed = true
	}

	if failed {
		_ = s.deps.Store.SetJobStatus(jobID, store.JobFailed)
	} else {
		_ = s.deps.Store.SetJobStatus(jobID, store.JobSucceeded)
	}
}

func ptrTrue() *bool { t := true; return &t }

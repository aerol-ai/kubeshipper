package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/aerol-ai/kubeshipper/internal/kube"
	"github.com/aerol-ai/kubeshipper/internal/store"

	"github.com/go-chi/chi/v5"
)

// /services is a streaming-only API. Every mutating call returns a jobId and
// an SSE stream URL. Read-only calls (list, get, logs) are plain JSON / log
// streams as before.
func (s *Server) mountServices(r chi.Router) {
	r.Route("/services", func(g chi.Router) {
		g.Post("/", s.createService)
		g.Get("/", s.listServices)

		// Job endpoints — must be registered BEFORE "/{id}" or chi's path
		// matching will route /jobs/<id> into getService.
		g.Get("/jobs/{id}", s.getServiceJob)
		g.Get("/jobs/{id}/stream", s.streamServiceJob)

		g.Get("/{id}", s.getService)
		g.Patch("/{id}", s.patchService)
		g.Delete("/{id}", s.deleteService)
		g.Post("/{id}/restart", s.restartService)
		g.Get("/{id}/logs", s.streamServiceLogs)
	})
}

// --- mutating handlers ---

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	spec, err := kube.ParseServiceSpec(body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "Validation failed", "details": err.Error()})
		return
	}
	if _, err := s.deps.Kube.ResolveNamespace(spec.Namespace); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	raw, _ := json.Marshal(spec)
	if err := s.deps.Store.UpsertService(spec.Name, raw, store.StatusPending); err != nil {
		writeJSON(w, 500, map[string]string{"error": "store write failed", "details": err.Error()})
		return
	}

	jobID := s.startJob(w, spec.Name, spec.Namespace, "deploy", initiator(r), "Service create requested via API")
	if jobID == "" {
		return
	}
	writeJobAccepted(w, spec.Name, jobID)
}

func (s *Server) patchService(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, err := s.deps.Store.GetService(id)
	if err != nil || rec == nil {
		writeJSON(w, 404, map[string]string{"error": "Service not found"})
		return
	}

	body, _ := io.ReadAll(r.Body)
	var patch kube.ServiceSpec
	if err := json.Unmarshal(body, &patch); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON", "details": err.Error()})
		return
	}

	var existing kube.ServiceSpec
	_ = json.Unmarshal(rec.Spec, &existing)
	merged := existing.Merge(&patch)
	merged.Name = id
	if err := merged.Validate(); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	raw, _ := json.Marshal(merged)
	_ = s.deps.Store.UpsertService(id, raw, store.StatusPending)

	jobID := s.startJob(w, id, merged.Namespace, "patch", initiator(r), "Service patch requested via API")
	if jobID == "" {
		return
	}
	writeJobAccepted(w, id, jobID)
}

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, _ := s.deps.Store.GetService(id)
	if rec == nil {
		writeJSON(w, 404, map[string]string{"error": "Service not found"})
		return
	}
	var spec kube.ServiceSpec
	_ = json.Unmarshal(rec.Spec, &spec)

	jobID := s.startJob(w, id, spec.Namespace, "delete", initiator(r), "Service teardown requested via API")
	if jobID == "" {
		return
	}

	// Run the actual K8s delete inline; emit terminal event when done.
	go func() {
		if err := s.deps.Kube.DeleteService(r.Context(), id, spec.Namespace); err != nil {
			_ = s.deps.Store.AppendEvent(jobID, store.Event{
				Phase: "error", Error: err.Error(),
			})
			_ = s.deps.Store.SetJobStatus(jobID, store.JobFailed)
			return
		}
		_ = s.deps.Store.DeleteService(id)
		_ = s.deps.Store.AppendEvent(jobID, store.Event{
			Phase: "done", Message: "service torn down",
		})
		_ = s.deps.Store.SetJobStatus(jobID, store.JobSucceeded)
	}()

	writeJobAccepted(w, id, jobID)
}

func (s *Server) restartService(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, _ := s.deps.Store.GetService(id)
	if rec == nil {
		writeJSON(w, 404, map[string]string{"error": "Service not found"})
		return
	}
	var spec kube.ServiceSpec
	_ = json.Unmarshal(rec.Spec, &spec)

	jobID := s.startJob(w, id, spec.Namespace, "restart", initiator(r), "Manual rollout restart requested")
	if jobID == "" {
		return
	}

	// Patch the pod-template annotation, then let the worker watch the new rollout
	// once the user issues a follow-up. For restart, the rollout is driven by
	// Kubernetes itself; we just record completion synchronously.
	go func() {
		if err := s.deps.Kube.RestartService(r.Context(), id, spec.Namespace); err != nil {
			_ = s.deps.Store.AppendEvent(jobID, store.Event{
				Phase: "error", Error: err.Error(),
			})
			_ = s.deps.Store.SetJobStatus(jobID, store.JobFailed)
			return
		}
		_ = s.deps.Store.AppendEvent(jobID, store.Event{
			Phase: "done", Message: "restart annotation applied; Kubernetes is rolling pods",
		})
		_ = s.deps.Store.SetJobStatus(jobID, store.JobSucceeded)
	}()

	writeJobAccepted(w, id, jobID)
}

// --- read handlers ---

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	all, err := s.deps.Store.ListServices()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"services": all})
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, err := s.deps.Store.GetService(id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if rec == nil {
		writeJSON(w, 404, map[string]string{"error": "Service not found"})
		return
	}
	var spec kube.ServiceSpec
	_ = json.Unmarshal(rec.Spec, &spec)
	status, _ := s.deps.Kube.ServiceStatus(r.Context(), id, spec.Namespace)
	writeJSON(w, 200, map[string]any{
		"id":         rec.ID,
		"spec":       json.RawMessage(rec.Spec),
		"status":     rec.Status,
		"created_at": rec.CreatedAt,
		"updated_at": rec.UpdatedAt,
		"job_id":     rec.JobID,
		"k8sStatus":  status,
	})
}

func (s *Server) streamServiceLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, _ := s.deps.Store.GetService(id)
	if rec == nil {
		writeJSON(w, 404, map[string]string{"error": "Service not found"})
		return
	}
	var spec kube.ServiceSpec
	_ = json.Unmarshal(rec.Spec, &spec)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Transfer-Encoding", "chunked")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming unsupported"})
		return
	}

	pw := &flushingWriter{w: w, flusher: flusher}
	if err := s.deps.Kube.StreamPodLogs(r.Context(), spec.Namespace, id, pw); err != nil {
		_, _ = pw.Write([]byte("\n[error] " + err.Error() + "\n"))
	}
}

// --- job handlers ---

func (s *Server) getServiceJob(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) streamServiceJob(w http.ResponseWriter, r *http.Request) {
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
		body, _ := json.Marshal(ev)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
		flusher.Flush()
	}
	if j.Status == store.JobSucceeded || j.Status == store.JobFailed {
		_, _ = fmt.Fprintf(w, "event: end\ndata: {\"status\":%q}\n\n", string(j.Status))
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
			body, _ := json.Marshal(ev)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
			flusher.Flush()
			if ev.Phase == "complete" || ev.Phase == "error" {
				return
			}
		}
	}
}

// --- helpers ---

// startJob creates a job, attaches it to the service, seeds an initial
// validation event, and returns the jobID. On error it writes a 500 to w and
// returns "" (caller should bail out).
func (s *Server) startJob(w http.ResponseWriter, serviceID, namespace, op, initiator, msg string) string {
	ns := namespace
	if ns == "" {
		ns = "default"
	}
	jobID, err := s.deps.Store.CreateJob(serviceID, ns, op, initiator)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return ""
	}
	if err := s.deps.Store.AttachJob(serviceID, jobID); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return ""
	}
	_ = s.deps.Store.SetJobStatus(jobID, store.JobRunning)
	_ = s.deps.Store.AppendEvent(jobID, store.Event{Phase: "validation", Message: msg})
	return jobID
}

func writeJobAccepted(w http.ResponseWriter, serviceID, jobID string) {
	writeJSON(w, 202, map[string]string{
		"id":     serviceID,
		"jobId":  jobID,
		"status": "PENDING",
		"stream": "/services/jobs/" + jobID + "/stream",
	})
}

type flushingWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (fw *flushingWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	fw.flusher.Flush()
	return n, err
}

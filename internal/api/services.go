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

func (s *Server) mountServices(r chi.Router) {
	r.Route("/services", func(g chi.Router) {
		g.Post("/", s.createService)
		g.Get("/", s.listServices)

		// Service-job streaming endpoints — these must be registered BEFORE
		// the "/{id}" routes or chi will route /jobs/{id} into getService.
		g.Get("/jobs/{id}", s.getServiceJob)
		g.Get("/jobs/{id}/stream", s.streamServiceJob)

		g.Get("/{id}", s.getService)
		g.Patch("/{id}", s.patchService)
		g.Delete("/{id}", s.deleteService)
		g.Post("/{id}/restart", s.restartService)
		g.Get("/{id}/events", s.getServiceEvents)
		g.Get("/{id}/logs", s.streamServiceLogs)
	})
}

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
	_ = s.deps.Store.LogEvent(spec.Name, "Created", "Service deployment requested via API")

	if r.URL.Query().Get("stream") == "true" {
		jobID, err := s.attachServiceJob(spec.Name, spec.Namespace, "deploy", initiator(r), "Service create requested via API")
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 202, map[string]string{
			"id":     spec.Name,
			"jobId":  jobID,
			"status": "PENDING",
			"stream": "/services/jobs/" + jobID + "/stream",
		})
		return
	}
	writeJSON(w, 202, map[string]string{"id": spec.Name, "status": "PENDING", "message": "accepted"})
}

// attachServiceJob creates a job row, links it to the service, and seeds an
// initial validation event. Worker.emit takes over from there.
func (s *Server) attachServiceJob(serviceID, namespace, op, initiator, msg string) (string, error) {
	ns := namespace
	if ns == "" {
		ns = "default"
	}
	jobID, err := s.deps.Store.CreateJob(serviceID, ns, op, initiator)
	if err != nil {
		return "", err
	}
	if err := s.deps.Store.AttachJob(serviceID, jobID); err != nil {
		return "", err
	}
	_ = s.deps.Store.SetJobStatus(jobID, store.JobRunning)
	_ = s.deps.Store.AppendEvent(jobID, store.Event{
		Phase: "validation", Message: msg,
	})
	return jobID, nil
}

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
		"k8sStatus":  status,
	})
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
	_ = s.deps.Store.LogEvent(id, "Updated", "Service spec patched via API")

	if r.URL.Query().Get("stream") == "true" {
		jobID, err := s.attachServiceJob(id, merged.Namespace, "patch", initiator(r), "Service patch requested via API")
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 202, map[string]string{
			"id":     id,
			"jobId":  jobID,
			"status": "PENDING",
			"stream": "/services/jobs/" + jobID + "/stream",
		})
		return
	}
	writeJSON(w, 202, map[string]string{"id": id, "status": "PENDING"})
}

// getServiceJob — returns full job state + accumulated events.
// Reuses the same store.Job shape as /charts/jobs/:id.
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

// streamServiceJob — SSE stream of a service deploy/patch job.
// Replays everything from events_jsonl on connect, then streams live until
// the worker emits a terminal phase (done/error) or the client disconnects.
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

	// Replay any persisted events first so reconnecting clients see the full history.
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

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, _ := s.deps.Store.GetService(id)
	if rec == nil {
		writeJSON(w, 404, map[string]string{"error": "Service not found"})
		return
	}
	var spec kube.ServiceSpec
	_ = json.Unmarshal(rec.Spec, &spec)
	_ = s.deps.Store.LogEvent(id, "Deleting", "Tear down requested via API")
	if err := s.deps.Kube.DeleteService(r.Context(), id, spec.Namespace); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.deps.Store.DeleteService(id)
	writeJSON(w, 200, map[string]string{"id": id, "message": "deleted"})
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
	_ = s.deps.Store.LogEvent(id, "Restarting", "Manual rollout restart requested")
	if err := s.deps.Kube.RestartService(r.Context(), id, spec.Namespace); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"id": id, "message": "restart triggered"})
}

func (s *Server) getServiceEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, _ := s.deps.Store.GetService(id)
	if rec == nil {
		writeJSON(w, 404, map[string]string{"error": "Service not found"})
		return
	}
	evts, err := s.deps.Store.ServiceEvents(id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"events": evts})
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

type flushingWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (fw *flushingWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	fw.flusher.Flush()
	return n, err
}

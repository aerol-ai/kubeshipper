package api

import (
	"encoding/json"
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

	// Namespace allow-list check up-front so users get a clear error before we hit K8s.
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
	writeJSON(w, 202, map[string]string{"id": spec.Name, "status": "PENDING", "message": "accepted"})
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
	writeJSON(w, 202, map[string]string{"id": id, "status": "PENDING"})
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

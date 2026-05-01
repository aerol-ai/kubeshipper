package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aerol-ai/kubeshipper/internal/rollout"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountRolloutWatches(r chi.Router) {
	r.Route("/rollout-watches", func(g chi.Router) {
		g.Post("/", s.registerRolloutWatch)
		g.Get("/", s.listRolloutWatches)
		g.Get("/targets", s.listRolloutWatchTargets)
		g.Get("/{id}", s.getRolloutWatch)
		g.Post("/{id}/enable", s.enableRolloutWatch)
		g.Post("/{id}/disable", s.disableRolloutWatch)
		g.Post("/{id}/sync", s.syncRolloutWatch)
		g.Post("/{id}/restart", s.restartRolloutWatch)
		g.Delete("/{id}", s.deleteRolloutWatch)
	})
}

func (s *Server) registerRolloutWatch(w http.ResponseWriter, r *http.Request) {
	if s.deps.Rollouts == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rollout watch manager is unavailable"})
		return
	}
	var req rollout.RegisterRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON", "details": err.Error()})
		return
	}
	out, err := s.deps.Rollouts.Register(r.Context(), req)
	if err != nil {
		status := rolloutStatusCode(err)
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	status := http.StatusCreated
	if !out.Created {
		status = http.StatusOK
	}
	writeJSON(w, status, out)
}

func (s *Server) listRolloutWatches(w http.ResponseWriter, r *http.Request) {
	watches, err := s.deps.Store.ListRolloutWatches()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"watches": watches})
}

func (s *Server) listRolloutWatchTargets(w http.ResponseWriter, r *http.Request) {
	if s.deps.Rollouts == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rollout watch manager is unavailable"})
		return
	}
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	out, err := s.deps.Rollouts.DiscoverTargets(r.Context(), namespace)
	if err != nil {
		status := rolloutStatusCode(err)
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getRolloutWatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	watch, err := s.deps.Store.GetRolloutWatch(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if watch == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "rollout watch not found"})
		return
	}
	writeJSON(w, http.StatusOK, watch)
}

func (s *Server) syncRolloutWatch(w http.ResponseWriter, r *http.Request) {
	if s.deps.Rollouts == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rollout watch manager is unavailable"})
		return
	}
	id := chi.URLParam(r, "id")
	out, err := s.deps.Rollouts.Sync(r.Context(), id)
	if err != nil {
		status := rolloutStatusCode(err)
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) enableRolloutWatch(w http.ResponseWriter, r *http.Request) {
	s.mutateRolloutWatchEnabled(w, r, true)
}

func (s *Server) disableRolloutWatch(w http.ResponseWriter, r *http.Request) {
	s.mutateRolloutWatchEnabled(w, r, false)
}

func (s *Server) mutateRolloutWatchEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	if s.deps.Rollouts == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rollout watch manager is unavailable"})
		return
	}
	id := chi.URLParam(r, "id")
	out, err := s.deps.Rollouts.SetEnabled(r.Context(), id, enabled)
	if err != nil {
		status := rolloutStatusCode(err)
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) restartRolloutWatch(w http.ResponseWriter, r *http.Request) {
	if s.deps.Rollouts == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rollout watch manager is unavailable"})
		return
	}
	id := chi.URLParam(r, "id")
	out, err := s.deps.Rollouts.Restart(r.Context(), id)
	if err != nil {
		status := rolloutStatusCode(err)
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteRolloutWatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	watch, err := s.deps.Store.GetRolloutWatch(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if watch == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "rollout watch not found"})
		return
	}
	if err := s.deps.Store.DeleteRolloutWatch(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func rolloutStatusCode(err error) int {
	if errors.Is(err, rollout.ErrWatchNotFound) {
		return http.StatusNotFound
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "required"):
		return http.StatusBadRequest
	case strings.Contains(msg, "multiple containers"):
		return http.StatusBadRequest
	case strings.Contains(msg, "not found"):
		return http.StatusNotFound
	case strings.Contains(msg, "namespace"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

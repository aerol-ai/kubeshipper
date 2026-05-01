package api

import (
	"net/http"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/helm"
	"github.com/aerol-ai/kubeshipper/internal/kube"
	"github.com/aerol-ai/kubeshipper/internal/rollout"
	"github.com/aerol-ai/kubeshipper/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Deps struct {
	Store     *store.Store
	Kube      *kube.Client
	Helm      *helm.Manager
	Rollouts  *rollout.Manager
	AuthToken string
	StartedAt string
	Version   string
}

type Server struct {
	deps Deps
}

func NewServer(deps Deps) *Server { return &Server{deps: deps} }

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(60 * time.Second))

	// Always-public endpoints
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"name":        "kubeshipper",
			"description": "Kubernetes deployment + Helm chart API (Go)",
			"docs": map[string]string{
				"services":        "/services",
				"charts":          "/charts",
				"rollout_watches": "/rollout-watches",
			},
		})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status":     "ok",
			"started_at": s.deps.StartedAt,
			"version":    s.deps.Version,
		})
	})

	// Auth-gated routes
	r.Group(func(g chi.Router) {
		g.Use(s.authMiddleware)
		s.mountServices(g)
		s.mountCharts(g)
		s.mountRolloutWatches(g)
	})

	return r
}

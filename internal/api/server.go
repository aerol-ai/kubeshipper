package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/helm"
	"github.com/aerol-ai/kubeshipper/internal/kube"
	"github.com/aerol-ai/kubeshipper/internal/rollout"
	"github.com/aerol-ai/kubeshipper/internal/store"
	"github.com/aerol-ai/kubeshipper/internal/ui"

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
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status":     "ok",
			"started_at": s.deps.StartedAt,
			"version":    s.deps.Version,
		})
	})

	r.Route("/api", func(api chi.Router) {
		api.Get("/", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{
				"name":        "kubeshipper",
				"description": "Kubernetes deployment + Helm chart API (Go)",
				"docs": map[string]string{
					"services":        "/api/services",
					"charts":          "/api/charts",
					"rollout_watches": "/api/rollout-watches",
					"auth":            "/api/auth",
				},
			})
		})
		api.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{
				"status":     "ok",
				"started_at": s.deps.StartedAt,
				"version":    s.deps.Version,
			})
		})
		s.mountAuthEndpoints(api)
		api.Group(func(g chi.Router) {
			g.Use(s.authMiddleware)
			s.mountServices(g)
			s.mountCharts(g)
			s.mountRolloutWatches(g)
		})
	})

	uiHandler := ui.Handler()
	r.Get("/", uiHandler.ServeHTTP)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		uiHandler.ServeHTTP(w, r)
	})

	return r
}

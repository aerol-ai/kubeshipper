package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/api"
	"github.com/aerol-ai/kubeshipper/internal/helm"
	"github.com/aerol-ai/kubeshipper/internal/kube"
	"github.com/aerol-ai/kubeshipper/internal/rollout"
	"github.com/aerol-ai/kubeshipper/internal/store"
	"github.com/aerol-ai/kubeshipper/internal/worker"
)

func main() {
	port := envOr("PORT", "3000")
	dbPath := envOr("DB_PATH", "kubeshipper.sqlite")

	managedRaw := os.Getenv("MANAGED_NAMESPACES")
	if strings.TrimSpace(managedRaw) == "" {
		log.Fatal("FATAL: MANAGED_NAMESPACES is not set. " +
			"Set to a comma-separated list of namespaces this service may deploy into, " +
			"or \"*\" to allow all namespaces (requires cluster-wide RBAC). " +
			"Example: MANAGED_NAMESPACES=default,production,staging")
	}
	managed := map[string]bool{}
	wildcard := false
	for _, n := range strings.Split(managedRaw, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if n == "*" {
			wildcard = true
			continue
		}
		managed[n] = true
	}
	if wildcard {
		managed = nil
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	kubeCli, err := kube.New(managed, wildcard)
	if err != nil {
		log.Fatalf("kube: %v", err)
	}

	helmMgr, err := helm.New(kubeCli, st)
	if err != nil {
		log.Fatalf("helm: %v", err)
	}

	rolloutMgr := rollout.NewManager(st, kubeCli)

	srv := api.NewServer(api.Deps{
		Store:     st,
		Kube:      kubeCli,
		Helm:      helmMgr,
		Rollouts:  rolloutMgr,
		AuthToken: os.Getenv("AUTH_TOKEN"),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Version:   envOr("APP_VERSION", "dev"),
	})

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	wkr := worker.New(st, kubeCli, rolloutMgr)
	go wkr.Run(ctx)

	go func() {
		log.Printf("kubeshipper: listening on :%s", port)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("kubeshipper: shutting down")

	shutdownCtx, c := context.WithTimeout(context.Background(), 15*time.Second)
	defer c()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func envOr(k, dflt string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return dflt
}

var _ = fmt.Sprint // keep import for future fmt usage

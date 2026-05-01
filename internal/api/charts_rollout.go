package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/helm"
	"github.com/aerol-ai/kubeshipper/internal/rollout"
	"github.com/aerol-ai/kubeshipper/internal/store"
)

func validateRolloutWatch(cfg *helm.RolloutWatchConfig) error {
	if cfg == nil {
		return nil
	}
	deployment := strings.TrimSpace(cfg.Deployment)
	service := strings.TrimSpace(cfg.Service)
	if deployment == "" && service == "" {
		return fmt.Errorf("rolloutWatch.deployment or rolloutWatch.service is required")
	}
	if deployment != "" && service != "" {
		return fmt.Errorf("rolloutWatch.deployment and rolloutWatch.service are aliases; provide only one")
	}
	return nil
}

func (s *Server) registerChartRolloutWatch(ctx context.Context, emit helm.EmitFn, namespace string, cfg *helm.RolloutWatchConfig) error {
	if cfg == nil {
		return nil
	}
	if err := validateRolloutWatch(cfg); err != nil {
		return err
	}
	if s.deps.Rollouts == nil {
		return fmt.Errorf("rollout watch manager is unavailable")
	}

	target := strings.TrimSpace(cfg.Deployment)
	if target == "" {
		target = strings.TrimSpace(cfg.Service)
	}
	if emit != nil {
		emit(store.Event{
			Phase:   "validation",
			Message: fmt.Sprintf("registering rollout watch for %s/%s", namespace, target),
			TS:      time.Now().UnixMilli(),
		})
	}

	out, err := s.deps.Rollouts.Register(ctx, rollout.RegisterRequest{
		Namespace:  namespace,
		Deployment: strings.TrimSpace(cfg.Deployment),
		Service:    strings.TrimSpace(cfg.Service),
		Container:  strings.TrimSpace(cfg.Container),
	})
	if err != nil {
		return fmt.Errorf("helm operation succeeded but rollout watch registration failed: %w", err)
	}

	verb := "registered"
	if !out.Created {
		verb = "refreshed"
	}
	message := fmt.Sprintf("rollout watch %s for %s/%s", verb, out.Watch.Namespace, out.Watch.Deployment)
	if out.Watch.Container != "" {
		message += fmt.Sprintf(" (container=%s)", out.Watch.Container)
	}
	if emit != nil {
		emit(store.Event{Phase: "done", Message: message, TS: time.Now().UnixMilli()})
	}
	return nil
}

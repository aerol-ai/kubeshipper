package api

import (
	"context"
	"testing"

	"strings"

	helmtypes "github.com/aerol-ai/kubeshipper/internal/helm"
	"github.com/aerol-ai/kubeshipper/internal/store"
	corev1 "k8s.io/api/core/v1"
)

func TestRegisterChartRolloutWatch_RegistersWatch(t *testing.T) {
	srv := newTestServer(t)
	seedDeployment(t, srv, "agent-gateway", containers("ghcr.io/acme/agent:latest"), true, apiOldDigest)

	events := []store.Event{}
	err := srv.registerChartRolloutWatch(context.Background(), func(ev store.Event) {
		events = append(events, ev)
	}, "default", &helmtypes.RolloutWatchConfig{Deployment: "agent-gateway"})
	if err != nil {
		t.Fatalf("register chart rollout watch: %v", err)
	}
	watches, err := srv.deps.Store.ListRolloutWatches()
	if err != nil {
		t.Fatalf("list rollout watches: %v", err)
	}
	if len(watches) != 1 {
		t.Fatalf("expected 1 rollout watch, got %d", len(watches))
	}
	if watches[0].Deployment != "agent-gateway" {
		t.Fatalf("deployment: got %q", watches[0].Deployment)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 emitted events, got %d", len(events))
	}
	if events[0].Phase != "validation" {
		t.Fatalf("first event phase: got %q", events[0].Phase)
	}
	if events[1].Phase != "done" {
		t.Fatalf("second event phase: got %q", events[1].Phase)
	}
}

func TestRegisterChartRolloutWatch_RefreshesExistingWatch(t *testing.T) {
	srv := newTestServer(t)
	seedDeployment(t, srv, "agent-gateway", containers("ghcr.io/acme/agent:latest"), true, apiOldDigest)

	firstCfg := &helmtypes.RolloutWatchConfig{Deployment: "agent-gateway"}
	if err := srv.registerChartRolloutWatch(context.Background(), nil, "default", firstCfg); err != nil {
		t.Fatalf("first register: %v", err)
	}

	events := []store.Event{}
	err := srv.registerChartRolloutWatch(context.Background(), func(ev store.Event) {
		events = append(events, ev)
	}, "default", &helmtypes.RolloutWatchConfig{Service: "agent-gateway"})
	if err != nil {
		t.Fatalf("second register: %v", err)
	}
	watches, err := srv.deps.Store.ListRolloutWatches()
	if err != nil {
		t.Fatalf("list rollout watches: %v", err)
	}
	if len(watches) != 1 {
		t.Fatalf("expected watch to be refreshed in place, got %d watches", len(watches))
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 emitted events, got %d", len(events))
	}
	if events[1].Message == "" || events[1].Phase != "done" {
		t.Fatalf("unexpected refresh event: %#v", events[1])
	}
	if want := "rollout watch refreshed for default/agent-gateway"; !strings.HasPrefix(events[1].Message, want) {
		t.Fatalf("refresh event: got %q, want prefix %q", events[1].Message, want)
	}
}

func TestRegisterChartRolloutWatch_MissingTarget(t *testing.T) {
	srv := newTestServer(t)
	err := srv.registerChartRolloutWatch(context.Background(), nil, "default", &helmtypes.RolloutWatchConfig{})
	if err == nil {
		t.Fatal("expected missing-target error")
	}
}

func containers(image string) []corev1.Container {
	return []corev1.Container{{Name: "app", Image: image}}
}

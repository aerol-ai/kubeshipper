package helm

import (
	"context"
	"fmt"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/helm/source"
	"github.com/aerol-ai/kubeshipper/internal/store"

	"helm.sh/helm/v3/pkg/action"
)

// Install runs the full install pipeline: validate → fetch chart → provision
// prereq secrets → helm install with optional --atomic --wait. Streams events.
func (m *Manager) Install(ctx context.Context, req *InstallReq, emit EmitFn) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	emitFn(emit, "validation", "starting install")

	if req.Release == "" || req.Namespace == "" {
		emitErrFn(emit, "release and namespace are required")
		return fmt.Errorf("release and namespace are required")
	}

	cfg, err := m.actionConfig(req.Namespace)
	if err != nil {
		emitErrFn(emit, fmt.Sprintf("action config: %v", err))
		return err
	}

	emitFn(emit, "validation", "fetching chart")
	ch, err := source.Fetch(toSourceReq(req.Source))
	if err != nil {
		emitErrFn(emit, fmt.Sprintf("fetch chart: %v", err))
		return err
	}

	if req.Prerequisites != nil && len(req.Prerequisites.Secrets) > 0 {
		emitFn(emit, "prereqs", fmt.Sprintf("provisioning %d prerequisite secret(s)", len(req.Prerequisites.Secrets)))
		if err := m.applyPrereqSecrets(ctx, req.Prerequisites.Secrets); err != nil {
			emitErrFn(emit, fmt.Sprintf("prereq secrets: %v", err))
			return err
		}
	}

	valuesYAML, _ := valuesToYAML(req.Values)
	values, err := parseValuesYAML(valuesYAML)
	if err != nil {
		emitErrFn(emit, fmt.Sprintf("parse values: %v", err))
		return err
	}

	install := action.NewInstall(cfg)
	install.ReleaseName = req.Release
	install.Namespace = req.Namespace
	install.Atomic = boolDefault(req.Atomic, true)
	install.Wait = boolDefault(req.Wait, true)
	install.CreateNamespace = boolDefault(req.CreateNamespace, true)
	install.Timeout = timeoutOrDefault(req.TimeoutSeconds, 10*time.Minute)

	emitFn(emit, "apply", fmt.Sprintf("installing %s/%s (chart=%s version=%s)",
		req.Namespace, req.Release, ch.Name(), ch.Metadata.Version))

	rel, err := install.RunWithContext(ctx, ch, values)
	if err != nil {
		emitErrFn(emit, fmt.Sprintf("install: %v", err))
		return err
	}

	emitFn(emit, "done", fmt.Sprintf("revision=%d status=%s", rel.Version, rel.Info.Status))
	return nil
}

// helpers — local aliases so each handler reads cleanly without importing util in every file
func emitFn(e EmitFn, phase, msg string) {
	if e != nil {
		e(store.Event{Phase: phase, Message: msg, TS: time.Now().UnixMilli()})
	}
}

func emitErrFn(e EmitFn, msg string) {
	if e != nil {
		e(store.Event{Phase: "error", Error: msg, TS: time.Now().UnixMilli()})
	}
}
